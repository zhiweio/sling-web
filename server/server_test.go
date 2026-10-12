package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/slingdata-io/sling-cli/core/env"
)

// testEnv points SLING_HOME_DIR at a temp dir so tests never touch the real
// ~/.sling (keys, dbs). Unlike t.TempDir, cleanup is best-effort: SQLite on
// Windows keeps the file locked past Close, and failing the test over a
// temp-dir unlink is noise, not signal.
func testEnv(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sling-server-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLING_HOME_DIR", dir)
	env.LoadHomeDir()
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := testEnv(t)
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

/* --------------------------------- vault ---------------------------------- */

func TestVaultRoundTrip(t *testing.T) {
	db := openTestDB(t)
	v, err := NewVault(db, "")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}

	if err = v.SaveSecret("conn/pg1/password", "s3cr3t!"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := v.LoadSecret("conn/pg1/password")
	if err != nil || got != "s3cr3t!" {
		t.Fatalf("load = %q, err=%v", got, err)
	}

	// overwrite
	v.SaveSecret("conn/pg1/password", "new-pass")
	got, _ = v.LoadSecret("conn/pg1/password")
	if got != "new-pass" {
		t.Fatalf("overwrite failed: %q", got)
	}

	// missing -> empty
	got, err = v.LoadSecret("conn/nope/x")
	if err != nil || got != "" {
		t.Fatalf("missing = %q err=%v", got, err)
	}

	// ciphertext at rest is not plaintext
	rec := SecretRecord{}
	db.First(&rec, "path = ?", "conn/pg1/password")
	if string(rec.Ciphertext) == "new-pass" || len(rec.Ciphertext) < 12+8 {
		t.Fatalf("ciphertext looks like plaintext")
	}

	// tampered ciphertext fails (GCM auth)
	rec.Ciphertext[0] ^= 0xFF
	db.Save(&rec)
	if _, err = v.LoadSecret("conn/pg1/password"); err == nil {
		t.Fatalf("tampered ciphertext should not decrypt")
	}
}

func TestVaultWrongKeyFails(t *testing.T) {
	dir := testEnv(t)
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := NewVault(db, "")
	v.SaveSecret("conn/a/password", "hunter2")

	// simulate a different machine: fresh key file
	os.Remove(filepath.Join(dir, "server.key"))
	db2, _ := OpenDB(filepath.Join(dir, "test.db"))
	defer db2.Close()
	v2, err := NewVault(db2, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := v2.LoadSecret("conn/a/password"); err == nil && got == "hunter2" {
		t.Fatalf("decrypted with wrong key: should fail")
	}
}

func TestVaultPassphraseFlow(t *testing.T) {
	dir := testEnv(t)
	db, _ := OpenDB(filepath.Join(dir, "test.db"))

	v, err := NewVault(db, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	v.SaveSecret("conn/b/token", "tok123")

	// key file must now be wrapped (not raw 32 bytes)
	raw, _ := os.ReadFile(filepath.Join(dir, "server.key"))
	if len(raw) == 32 {
		t.Fatalf("key file should be wrapped json, got raw key")
	}

	// wrong passphrase rejected
	if _, err = NewVault(db, "wrong"); err == nil {
		t.Fatalf("wrong passphrase must fail")
	}

	// right passphrase works (fresh handle, same db)
	db2, _ := OpenDB(filepath.Join(dir, "test.db"))
	defer db2.Close()
	v2, err := NewVault(db2, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v2.LoadSecret("conn/b/token"); got != "tok123" {
		t.Fatalf("passphrase vault round trip failed: %q", got)
	}
}

/* ---------------------------------- spec ----------------------------------- */

func TestSpecModeMapping(t *testing.T) {
	cases := []struct {
		ui   string
		pk   []string
		uk   string
		want string
	}{
		{"replace", nil, "", "full-refresh"},
		{"truncate", nil, "", "truncate"},
		{"append", nil, "updated_at", "incremental"},
		{"append", nil, "", "full-refresh"}, // no key -> degrade
		{"upsert", []string{"id"}, "", "incremental"},
		{"upsert", nil, "", "full-refresh"}, // no pk -> degrade
		{"snapshot", nil, "", "snapshot"},
	}
	for _, c := range cases {
		if got := slingMode(c.ui, c.pk, c.uk); string(got) != c.want {
			t.Errorf("slingMode(%q,%v,%q) = %q want %q", c.ui, c.pk, c.uk, got, c.want)
		}
	}
}

func TestSpecYAMLExportImport(t *testing.T) {
	spec := JobSpec{
		SourceConn: "SRC",
		TargetConn: "TGT",
		Defaults:   StreamSpec{Mode: "upsert", PrimaryKey: []string{"id"}, UpdateKey: "updated_at"},
		Streams: []StreamSpec{
			{SourceObject: "public.users", TargetObject: "main.users"},
			{SourceObject: "public.orders", TargetObject: "main.orders", Mode: "truncate"},
		},
	}
	yamlStr, err := spec.ToYAML()
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(yamlStr, "primary_key: id") || !containsStr(yamlStr, "mode: incremental") || !containsStr(yamlStr, "object: main.orders") {
		t.Fatalf("yaml missing fields:\n%s", yamlStr)
	}

	back, err := SpecFromReplicationYAML(yamlStr)
	if err != nil {
		t.Fatal(err)
	}
	if back.SourceConn != "SRC" || back.TargetConn != "TGT" {
		t.Fatalf("round trip conns: %+v", back)
	}
	if len(back.Streams) != 2 {
		t.Fatalf("round trip streams: %d", len(back.Streams))
	}
	if back.Defaults.Mode != "upsert" {
		t.Fatalf("round trip mode: %q", back.Defaults.Mode)
	}
}

func TestSpecSingleVsReplication(t *testing.T) {
	single := JobSpec{Streams: []StreamSpec{{SourceObject: "a.b"}}}
	if !single.IsSingleTask() {
		t.Error("single stream should be a task")
	}
	multi := JobSpec{Streams: []StreamSpec{{SourceObject: "a.b"}, {SourceObject: "a.c"}}}
	if multi.IsSingleTask() {
		t.Error("two streams should be a replication")
	}
	wild := JobSpec{Streams: []StreamSpec{{SourceObject: "public.*"}}}
	if wild.IsSingleTask() {
		t.Error("wildcard should be a replication")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
