// vault.go implements the encrypted connection secret store.
//
// Design:
//   - Master key: 32 random bytes in <home>/server.key (0600). Optionally the
//     key file is "wrapped": the master key is encrypted under a key derived
//     from a user passphrase (Argon2id), so a stolen key file alone is useless.
//   - Values: AES-256-GCM, fresh 12-byte nonce prefixed to each ciphertext,
//     one row per secret path (conn/<name>/<field>).
//   - Plaintext exists only in process memory.
package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/env"
	"golang.org/x/crypto/argon2"
)

const keyFileSize = 32

// wrappedKeyFile is the on-disk format when a passphrase protects the master key.
type wrappedKeyFile struct {
	V       int    `json:"v"`
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"t"`
	Memory  uint32 `json:"m"`
	Threads uint8  `json:"p"`
	Wrapped []byte `json:"wrapped"` // nonce||ciphertext of the 32-byte master key
}

func homeDir() string { return env.HomeDir }

func keyFilePath() string { return filepath.Join(homeDir(), "server.key") }

// Vault encrypts and stores connection secrets.
type Vault struct {
	db  *DB
	key []byte
}

// NewVault loads (or creates) the master key and returns a vault bound to db.
// When passphrase is non-empty and the key is wrapped, it unwraps; when the
// key is raw and a passphrase is given for the first time, it wraps it.
func NewVault(db *DB, passphrase string) (*Vault, error) {
	path := keyFilePath()
	key, err := loadOrCreateKey(path, passphrase)
	if err != nil {
		return nil, err
	}
	return &Vault{db: db, key: key}, nil
}

func loadOrCreateKey(path, passphrase string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, g.Error(err, "could not read key file %s", path)
		}
		// first run: generate
		key := make([]byte, keyFileSize)
		if _, err = rand.Read(key); err != nil {
			return nil, g.Error(err, "could not generate master key")
		}
		if passphrase == "" {
			if err = os.WriteFile(path, key, 0600); err != nil {
				return nil, g.Error(err, "could not write key file %s", path)
			}
			return key, nil
		}
		wrapped, err := wrapKey(key, passphrase)
		if err != nil {
			return nil, err
		}
		if err = os.WriteFile(path, wrapped, 0600); err != nil {
			return nil, g.Error(err, "could not write key file %s", path)
		}
		return key, nil
	}

	// existing key: raw (len 32) or wrapped (JSON)
	if len(raw) == keyFileSize {
		if passphrase == "" {
			return raw, nil
		}
		// upgrade raw -> wrapped in place
		wrapped, err := wrapKey(raw, passphrase)
		if err != nil {
			return nil, err
		}
		if err = os.WriteFile(path, wrapped, 0600); err != nil {
			return nil, g.Error(err, "could not wrap key file %s", path)
		}
		return raw, nil
	}

	wk := wrappedKeyFile{}
	if err = json.Unmarshal(raw, &wk); err != nil {
		return nil, g.Error(err, "key file %s is neither raw nor wrapped format", path)
	}
	if passphrase == "" {
		return nil, g.Error("key file %s is passphrase-protected: provide --passphrase", path)
	}
	kek := argon2.IDKey([]byte(passphrase), wk.Salt, wk.Time, wk.Memory, wk.Threads, keyFileSize)
	plain, err := gcmOpen(kek, wk.Wrapped)
	if err != nil {
		return nil, g.Error(err, "wrong passphrase for key file %s", path)
	}
	return plain, nil
}

func wrapKey(key []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, g.Error(err, "could not generate salt")
	}
	const t, m, p = 1, 64 * 1024, 4 // 64 MiB, 4 lanes: ~sub-second, memory-hard
	kek := argon2.IDKey([]byte(passphrase), salt, t, m, p, keyFileSize)
	wrapped, err := gcmSeal(kek, key)
	if err != nil {
		return nil, g.Error(err, "could not wrap master key")
	}
	out, err := json.Marshal(wrappedKeyFile{V: 1, Salt: salt, Time: t, Memory: m, Threads: p, Wrapped: wrapped})
	if err != nil {
		return nil, g.Error(err, "could not encode key file")
	}
	return out, nil
}

func gcmSeal(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func gcmOpen(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, g.Error("ciphertext too short")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}

/* ------------------------------ secret rows ------------------------------ */

// SaveSecret encrypts and upserts a secret at path (e.g. conn/mypg/password).
func (v *Vault) SaveSecret(path, plaintext string) error {
	if plaintext == "" {
		return nil
	}
	ct, err := gcmSeal(v.key, []byte(plaintext))
	if err != nil {
		return g.Error(err, "could not encrypt secret %s", path)
	}
	return v.db.Save(&SecretRecord{Path: path, Ciphertext: ct, UpdatedAt: time.Now()}).Error
}

// LoadSecret decrypts a secret; returns "" when absent.
func (v *Vault) LoadSecret(path string) (string, error) {
	rec := SecretRecord{}
	if err := v.db.First(&rec, "path = ?", path).Error; err != nil {
		return "", nil
	}
	plain, err := gcmOpen(v.key, rec.Ciphertext)
	if err != nil {
		return "", g.Error(err, "could not decrypt secret %s (wrong key file?)", path)
	}
	return string(plain), nil
}

// DeleteSecrets removes all secrets under a prefix (e.g. conn/mypg/).
func (v *Vault) DeleteSecrets(prefix string) error {
	return v.db.Where("path LIKE ?", prefix+"%").Delete(&SecretRecord{}).Error
}

// secretPath builds the canonical path of a connection secret field.
func secretPath(connName, field string) string {
	return g.F("conn/%s/%s", connName, field)
}
