// auth.go implements the single-admin authentication model: credential
// bootstrap, bcrypt verification, login throttling and in-memory sessions.
//
// The account always exists: a deployment-configured password (--admin-password
// / SLING_SERVER_ADMIN_PASSWORD) wins and replaces the stored hash at boot;
// otherwise the stored account is kept; otherwise the default admin/admin is
// seeded with a must-change flag enforced at the API boundary.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/flarco/g"
	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultAdminUser / DefaultAdminPass seed the account when the deployment
	// does not configure one. The first login must change the password before
	// the API serves anything else.
	DefaultAdminUser = "admin"
	DefaultAdminPass = "admin"

	// sessionCookie is HttpOnly + SameSite=Lax; sessions live in memory only,
	// so a server restart invalidates them all.
	sessionCookie = "sling_session"
	sessionTTL    = 7 * 24 * time.Hour
)

/* -------------------------------- sessions -------------------------------- */

// session is one authenticated browser session.
type session struct {
	Username   string
	MustChange bool
	CreatedAt  time.Time
	LastSeen   time.Time
}

// sessionStore is a mutex-guarded token -> session map with a sliding TTL.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: map[string]*session{}}
}

func (s *sessionStore) create(username string, mustChange bool) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing leaves no safe way to issue tokens
		panic("could not generate session token: " + err.Error())
	}
	token := hex.EncodeToString(b)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = &session{Username: username, MustChange: mustChange, CreatedAt: now, LastSeen: now}
	for t, sess := range s.sessions { // opportunistic janitor
		if now.Sub(sess.LastSeen) > sessionTTL {
			delete(s.sessions, t)
		}
	}
	return token
}

// get returns the live session for token, sliding its expiry, or nil when
// absent or expired.
func (s *sessionStore) get(token string) *session {
	if token == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return nil
	}
	if time.Since(sess.LastSeen) > sessionTTL {
		delete(s.sessions, token)
		return nil
	}
	sess.LastSeen = time.Now()
	return sess
}

// clearMustChange marks the session as having completed its password change.
func (s *sessionStore) clearMustChange(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[token]; sess != nil {
		sess.MustChange = false
	}
}

// rotate issues a fresh token carrying the same identity and drops the old
// one; used after a password change to defeat session fixation.
func (s *sessionStore) rotate(oldToken string) string {
	s.mu.Lock()
	sess := s.sessions[oldToken]
	s.mu.Unlock()
	if sess == nil {
		return ""
	}
	token := s.create(sess.Username, sess.MustChange)
	s.mu.Lock()
	delete(s.sessions, oldToken)
	s.mu.Unlock()
	return token
}

func (s *sessionStore) drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// dropAll invalidates every session (e.g. after the password changed via CLI).
func (s *sessionStore) dropAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]*session{}
}

/* ------------------------------- throttling ------------------------------- */

const (
	throttleThreshold = 5              // free failures before a lockout kicks in
	throttleBase      = 30 * time.Second
	throttleMax       = 15 * time.Minute
)

// loginThrottle enforces an exponential lockout after consecutive failures
// (5 failures -> 30s, each further failure doubles, capped at 15 min).
type loginThrottle struct {
	mu          sync.Mutex
	failures    int
	lockedUntil time.Time
}

// check returns the remaining lockout; zero means an attempt may proceed.
func (t *loginThrottle) check() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d := time.Until(t.lockedUntil); d > 0 {
		return d
	}
	return 0
}

func (t *loginThrottle) fail() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures++
	if t.failures >= throttleThreshold {
		d := throttleBase << uint(t.failures-throttleThreshold)
		if d > throttleMax || d <= 0 {
			d = throttleMax
		}
		t.lockedUntil = time.Now().Add(d)
		return d
	}
	return 0
}

func (t *loginThrottle) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = 0
	t.lockedUntil = time.Time{}
}

/* ---------------------------- credential store ---------------------------- */

// bootstrapAdmin seeds or updates the admin account at startup. Call once
// from New, before any request can reach the API.
func (a *App) bootstrapAdmin() error {
	user := a.adminUsername()
	if a.Opt.AdminPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(a.Opt.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return g.Error(err, "could not hash admin password")
		}
		rec := AdminUser{Username: user, PasswordHash: string(hash)}
		if err := a.DB.Save(&rec).Error; err != nil {
			return g.Error(err, "could not store admin account")
		}
		a.DB.Audit("update", "admin", user, "password set via deployment config")
		return nil
	}

	var rec AdminUser
	if err := a.DB.First(&rec, "username = ?", user).Error; err == nil {
		return nil // keep the stored credential across restarts
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultAdminPass), bcrypt.DefaultCost)
	if err != nil {
		return g.Error(err, "could not hash default admin password")
	}
	rec = AdminUser{Username: user, PasswordHash: string(hash), MustChangePassword: true}
	if err := a.DB.Create(&rec).Error; err != nil {
		return g.Error(err, "could not seed default admin account")
	}
	g.Warn("no admin password configured: seeded default account '%s/%s' - the password must be changed at first login (or set SLING_SERVER_ADMIN_PASSWORD)",
		DefaultAdminUser, DefaultAdminPass)
	return nil
}

func (a *App) adminUsername() string {
	if a.Opt.AdminUsername != "" {
		return a.Opt.AdminUsername
	}
	return DefaultAdminUser
}

// HasDefaultCredentials reports whether the admin account still carries the
// seeded default password (the must-change flag is its marker).
func (a *App) HasDefaultCredentials() bool {
	var rec AdminUser
	if err := a.DB.First(&rec, "username = ?", a.adminUsername()).Error; err != nil {
		return false
	}
	return rec.MustChangePassword
}

// verifyLogin checks credentials under the lockout window. ok is false for
// unknown users and bad passwords alike; mustChange reports whether the
// session may only change the password.
func (a *App) verifyLogin(username, password string) (mustChange, ok bool) {
	var rec AdminUser
	err := a.DB.First(&rec, "username = ?", username).Error
	if err != nil {
		// burn comparable bcrypt time so a missing user is not distinguishable
		// from a bad password by timing alone
		bcrypt.CompareHashAndPassword([]byte("x"), []byte(password))
		a.DB.Audit("login", "admin", username, "failed: unknown user")
		return false, false
	}
	if bcrypt.CompareHashAndPassword([]byte(rec.PasswordHash), []byte(password)) != nil {
		a.DB.Audit("login", "admin", username, "failed: bad password")
		return false, false
	}
	a.DB.Audit("login", "admin", username, "success")
	return rec.MustChangePassword, true
}

// changePassword verifies the current password, stores the new hash and
// clears the must-change flag.
func (a *App) changePassword(username, current, next string) error {
	var rec AdminUser
	if err := a.DB.First(&rec, "username = ?", username).Error; err != nil {
		return g.Error("invalid account")
	}
	if bcrypt.CompareHashAndPassword([]byte(rec.PasswordHash), []byte(current)) != nil {
		return g.Error("current password is invalid")
	}
	if len(next) < 8 {
		return g.Error("new password is invalid: must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return g.Error(err, "could not hash password")
	}
	rec.PasswordHash = string(hash)
	rec.MustChangePassword = false
	if err := a.DB.Save(&rec).Error; err != nil {
		return g.Error(err, "could not store password")
	}
	a.DB.Audit("update", "admin", username, "password changed")
	return nil
}

/* ------------------------------ request plumbing --------------------------- */

// sessionFrom returns the live session bound to the request cookie, if any.
func (a *App) sessionFrom(r *http.Request) *session {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return a.Sessions.get(c.Value)
}

// bearerToken extracts the API token from the Authorization header, falling
// back to ?token= for contexts that cannot set headers (EventSource, file
// download links).
func bearerToken(r *http.Request) string {
	t := r.Header.Get("Authorization")
	if t == "" {
		return r.URL.Query().Get("token")
	}
	return strings.TrimPrefix(t, "Bearer ")
}

// authExempt lists the paths reachable without credentials: the SPA shell and
// static assets (the UI itself gates on the session probe), the login
// exchange, the session probe, and the health check used by the Docker
// healthcheck and the single-instance probe, which hold no cookie.
func authExempt(r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/api/") {
		return true
	}
	switch p {
	case "/api/auth/login", "/api/auth/session", "/api/health":
		return true
	}
	return false
}

// authMustChangeAllowed lists the paths a session that still owes a password
// change may take: the change itself, logout, and the session probe.
func authMustChangeAllowed(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/auth/password", "/api/auth/logout", "/api/auth/session":
		return true
	}
	return false
}

func mutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// crossSiteMutation reports whether a browser-declared cross-site mutating
// request should be rejected (CSRF defense-in-depth on top of SameSite=Lax).
// The Sec-Fetch-Site header is emitted by all modern browsers regardless of
// proxying, so it stays correct in the Vite dev setup where an Origin check
// would misfire (browser origin :5173 vs server host :5666).
func crossSiteMutation(r *http.Request) bool {
	if !mutatingMethod(r.Method) {
		return false
	}
	return r.Header.Get("Sec-Fetch-Site") == "cross-site"
}
