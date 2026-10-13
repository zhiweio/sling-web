package server

// auth_test.go: login/logout/session, the must-change gate, the lockout and
// credential persistence across restarts. Default credentials are admin/admin
// (seeded when no Options.AdminPassword is set) — the must-change flag makes
// every non-auth endpoint answer 403 until the password changes.

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// bareClient is a cookie-less client for unauthenticated requests.
func bareClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// newDefaultAuthApp builds an app with no configured password, so the
// default admin/admin + must-change state the auth tests exercise is seeded.
func newDefaultAuthApp(t *testing.T) *App {
	t.Helper()
	dir := testEnv(t)
	app, err := New(Options{DBPath: dir + "\\test.db", Concurrency: 2})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(app.Shutdown)
	return app
}

// loginAs posts credentials and returns the status, body and Set-Cookie jar
// state of the client afterwards.
func loginAs(t *testing.T, base, username, password string) (int, string, *http.Client) {
	t.Helper()
	client := bareClient()
	resp, err := client.Post(base+"/api/auth/login", "application/json",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := readAll(t, resp)
	return resp.StatusCode, string(b), client
}

func readAll(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf
}

func getAs(t *testing.T, client *http.Client, base, path string) (int, string) {
	t.Helper()
	resp, err := client.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, string(readAll(t, resp))
}

func postAs(t *testing.T, client *http.Client, base, path, body string) (int, string) {
	t.Helper()
	resp, err := client.Post(base+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, string(readAll(t, resp))
}

func TestAuthLoginSessionLogout(t *testing.T) {
	app := newDefaultAuthApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// unauthenticated probe reports no session
	client := bareClient()
	if resp, body := getAs(t, client, ts.URL, "/api/auth/session"); resp != 200 || !strings.Contains(body, `"authenticated":false`) {
		t.Fatalf("session probe: %d %s", resp, body)
	}
	// protected endpoint without credentials -> 401
	if resp, _ := getAs(t, client, ts.URL, "/api/version"); resp != 401 {
		t.Fatalf("no session = %d, want 401", resp)
	}

	// wrong password is a uniform 401
	if resp, body, _ := loginAs(t, ts.URL, "admin", "wrong"); resp != 401 || !strings.Contains(body, "invalid username or password") {
		t.Fatalf("bad login: %d %s", resp, body)
	}

	// default credentials work and demand a change
	resp, body, client := loginAs(t, ts.URL, "admin", "admin")
	if resp != 200 || !strings.Contains(body, `"must_change_password":true`) {
		t.Fatalf("login: %d %s", resp, body)
	}
	if resp, body := getAs(t, client, ts.URL, "/api/auth/session"); resp != 200 || !strings.Contains(body, `"authenticated":true`) {
		t.Fatalf("session after login: %d %s", resp, body)
	}

	// logout clears the session
	if resp, _ := postAs(t, client, ts.URL, "/api/auth/logout", `{}`); resp != 200 {
		t.Fatalf("logout: %d", resp)
	}
	if resp, _ := getAs(t, client, ts.URL, "/api/version"); resp != 401 {
		t.Fatalf("after logout = %d, want 401", resp)
	}
}

func TestAuthMustChangeGate(t *testing.T) {
	app := newDefaultAuthApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	_, _, client := loginAs(t, ts.URL, "admin", "admin")

	// every non-auth endpoint is gated behind the password change
	if resp, body := getAs(t, client, ts.URL, "/api/jobs"); resp != 403 || !strings.Contains(body, "password change required") {
		t.Fatalf("gated endpoint: %d %s", resp, body)
	}
	if resp, _ := postAs(t, client, ts.URL, "/api/jobs", `{}`); resp != 403 {
		t.Fatalf("gated mutation: %d, want 403", resp)
	}
	// the session probe stays reachable
	if resp, _ := getAs(t, client, ts.URL, "/api/auth/session"); resp != 200 {
		t.Fatalf("session probe gated: %d", resp)
	}

	// short passwords rejected
	if resp, body := postAs(t, client, ts.URL, "/api/auth/password", `{"current_password":"admin","new_password":"short"}`); resp != 400 {
		t.Fatalf("short password: %d %s", resp, body)
	}
	// wrong current password rejected
	if resp, _ := postAs(t, client, ts.URL, "/api/auth/password", `{"current_password":"nope","new_password":"longenough1"}`); resp != 400 {
		t.Fatalf("wrong current password: %d", resp)
	}

	// a real change clears the gate (and the handler rotates the session
	// token, so the client's jar now carries the fresh one)
	if resp, body := postAs(t, client, ts.URL, "/api/auth/password", `{"current_password":"admin","new_password":"hunter2boss"}`); resp != 200 {
		t.Fatalf("change password: %d %s", resp, body)
	}
	if resp, _ := getAs(t, client, ts.URL, "/api/jobs"); resp != 200 {
		t.Fatalf("after change = %d, want 200", resp)
	}

	// the old default password no longer logs in; the new one does
	if resp, _, _ := loginAs(t, ts.URL, "admin", "admin"); resp != 401 {
		t.Fatalf("old password after change = %d, want 401", resp)
	}
	resp, body, client2 := loginAs(t, ts.URL, "admin", "hunter2boss")
	if resp != 200 || !strings.Contains(body, `"must_change_password":false`) {
		t.Fatalf("new login: %d %s", resp, body)
	}
	if resp, _ := getAs(t, client2, ts.URL, "/api/jobs"); resp != 200 {
		t.Fatalf("new session gated: %d", resp)
	}
}

func TestAuthLockout(t *testing.T) {
	app := newDefaultAuthApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// five free failures, then a lockout with a Retry-After hint
	var code int
	for i := 0; i < throttleThreshold; i++ {
		code, _, _ = loginAs(t, ts.URL, "admin", "wrong")
		if code != 401 {
			t.Fatalf("failure %d = %d, want 401", i+1, code)
		}
	}
	code, body, _ := loginAs(t, ts.URL, "admin", "wrong")
	if code != http.StatusTooManyRequests || !strings.Contains(body, "retry_after") {
		t.Fatalf("locked login = %d %s, want 429 with retry_after", code, body)
	}

	// even the correct password is refused while locked
	code, _, _ = loginAs(t, ts.URL, "admin", "admin")
	if code != http.StatusTooManyRequests {
		t.Fatalf("correct password while locked = %d, want 429", code)
	}

	// the lockout is time-based only: reset clears it
	app.LoginThrottle.reset()
	code, _, _ = loginAs(t, ts.URL, "admin", "admin")
	if code != 200 {
		t.Fatalf("after reset = %d, want 200", code)
	}
}

func TestAuthDefaultVsConfiguredPassword(t *testing.T) {
	dir := testEnv(t)
	dbPath := filepath.Join(dir, "test.db")

	// boot without config: default account seeded, flagged must-change
	app, err := New(Options{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if !app.HasDefaultCredentials() {
		t.Fatal("fresh deploy must report default credentials")
	}
	app.DB.Close()

	// deploy-time configuration replaces it and clears the flag
	app2, err := New(Options{DBPath: dbPath, AdminPassword: "deployment-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if app2.HasDefaultCredentials() {
		t.Fatal("configured password must clear the default flag")
	}
	app2.DB.Close()

	// restart without config: stored credential survives
	app3, err := New(Options{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer app3.Shutdown()
	if app3.HasDefaultCredentials() {
		t.Fatal("stored credential must survive restarts")
	}

	ts := httptest.NewServer(app3.Handler())
	defer ts.Close()
	if code, _, _ := loginAs(t, ts.URL, "admin", "admin"); code != 401 {
		t.Fatalf("default password = %d, want 401", code)
	}
	if code, body, _ := loginAs(t, ts.URL, "admin", "deployment-pass"); code != 200 || !strings.Contains(body, `"must_change_password":false`) {
		t.Fatalf("configured login: %d %s", code, body)
	}
}

func TestAuthCustomAdminUser(t *testing.T) {
	dir := testEnv(t)
	app, err := New(Options{DBPath: filepath.Join(dir, "test.db"), AdminUsername: "ops", AdminPassword: "root-pass-1"})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	// the configured account name works...
	if code, _, _ := loginAs(t, ts.URL, "ops", "root-pass-1"); code != 200 {
		t.Fatalf("custom user login = %d, want 200", code)
	}
	// ...and the default name is not an account at all
	if code, _, _ := loginAs(t, ts.URL, "admin", "admin"); code != 401 {
		t.Fatalf("default user with custom config = %d, want 401", code)
	}
}

func TestAuthSessionJSONShape(t *testing.T) {
	app := newDefaultAuthApp(t)
	ts := httptest.NewServer(app.Handler())
	defer ts.Close()

	_, body, client := loginAs(t, ts.URL, "admin", "admin")
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("login body not JSON: %s", body)
	}
	if m["username"] != "admin" || m["must_change_password"] != true {
		t.Fatalf("unexpected login payload: %s", body)
	}

	resp, err := client.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b := readAll(t, resp)
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("session body not JSON: %s", b)
	}
	if s["authenticated"] != true || s["must_change_password"] != true {
		t.Fatalf("unexpected session payload: %s", b)
	}
}
