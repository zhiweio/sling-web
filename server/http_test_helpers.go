package server

// http_test_helpers.go: tiny JSON HTTP helpers for httptest-based API tests.
//
// The bare api* helpers authenticate implicitly: they share a per-test-server
// http.Client whose cookie jar holds a logged-in admin session (the seeded
// default admin/admin). Use apiBare / doBare for deliberately unauthenticated
// requests (401 assertions, exempt-path checks).

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
)

var (
	authClientsMu sync.Mutex
	authClients   = map[string]*http.Client{} // base URL -> client with session jar
)

// testAdminPass is the deploy-time admin password every test app is
// constructed with, so the shared auto-login sessions carry no must-change
// gate (only the dedicated auth tests exercise the default-credential flow).
const testAdminPass = "test-admin-pass"

// authedClient returns an http.Client with a cookie jar carrying an admin
// session for base, logging in once with the configured test credentials.
func authedClient(t *testing.T, base string) *http.Client {
	t.Helper()
	authClientsMu.Lock()
	defer authClientsMu.Unlock()
	if c, ok := authClients[base]; ok {
		return c
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	body := `{"username":"admin","password":"` + testAdminPass + `"}`
	resp, err := client.Post(base+"/api/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("test login against %s: %d %s", base, resp.StatusCode, b)
	}
	authClients[base] = client
	return client
}

func doReq(t *testing.T, method, fullURL, token string, body any) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := jsonMarshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader([]byte(b))
	}
	req, err := http.NewRequest(method, fullURL, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	var client *http.Client
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		client = http.DefaultClient
	} else {
		u, err := url.Parse(fullURL)
		if err != nil {
			t.Fatal(err)
		}
		client = authedClient(t, u.Scheme + "://" + u.Host)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// doBare issues a request with no credentials at all.
func doBare(t *testing.T, method, fullURL string, body string, headers map[string]string) (int, string, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, fullURL, rdr)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func apiGet(t *testing.T, base, path string) (int, string) {
	return doReq(t, "GET", base+path, "", nil)
}

func apiGetAuth(t *testing.T, base, path, token string) (int, string) {
	return doReq(t, "GET", base+path, token, nil)
}

func apiPost(t *testing.T, base, path string, body any) (int, string) {
	return doReq(t, "POST", base+path, "", body)
}

func apiPut(t *testing.T, base, path string, body any) (int, string) {
	return doReq(t, "PUT", base+path, "", body)
}

func apiPatch(t *testing.T, base, path string, body any) (int, string) {
	return doReq(t, "PATCH", base+path, "", body)
}

func apiDelete(t *testing.T, base, path string) (int, string) {
	return doReq(t, "DELETE", base+path, "", nil)
}
