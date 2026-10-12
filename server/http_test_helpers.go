package server

// http_test_helpers.go: tiny JSON HTTP helpers for httptest-based API tests.

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func doReq(t *testing.T, method, url, token string, body any) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := jsonMarshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader([]byte(b))
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
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
