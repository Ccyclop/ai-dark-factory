package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "sub", "test.db"), true)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ts := httptest.NewServer(newServer(db))
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, method, url string, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, b
}

func assertJSONType(t *testing.T, resp *http.Response) {
	t.Helper()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func assertErrorBody(t *testing.T, b []byte) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", b, err)
	}
	if len(m) != 1 {
		t.Errorf("error body %s has %d fields, want 1", b, len(m))
	}
	s, ok := m["error"].(string)
	if !ok || s == "" {
		t.Errorf("error body %s: \"error\" is not a non-empty string", b)
	}
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	resp, b := do(t, http.MethodGet, ts.URL+"/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	assertJSONType(t, resp)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body %q: %v", b, err)
	}
	if len(m) != 1 || m["status"] != "ok" {
		t.Errorf("body = %s, want {\"status\": \"ok\"}", b)
	}
}

func TestHeadHealth(t *testing.T) {
	ts := newTestServer(t)
	resp, b := do(t, http.MethodHead, ts.URL+"/health", nil)
	if resp.StatusCode != http.StatusOK || len(b) != 0 {
		t.Errorf("HEAD /health = %d with %d body bytes, want 200 and none", resp.StatusCode, len(b))
	}
	assertJSONType(t, resp)
}

func TestNotFound(t *testing.T) {
	ts := newTestServer(t)
	for _, p := range []string{"/", "/nope", "/items/x/y/z", "/health/", "/Health", "//health", "/" + strings.Repeat("a", 8000)} {
		resp, b := do(t, http.MethodGet, ts.URL+p, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %.40s: status = %d, want 404", p, resp.StatusCode)
			continue
		}
		assertJSONType(t, resp)
		assertErrorBody(t, b)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	ts := newTestServer(t)
	for _, m := range []string{http.MethodPost, http.MethodDelete, http.MethodPut, http.MethodPatch, "BREW"} {
		resp, b := do(t, m, ts.URL+"/health", nil)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /health: status = %d, want 405", m, resp.StatusCode)
			continue
		}
		if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
			t.Errorf("%s /health: Allow = %q, want it to include GET", m, allow)
		}
		assertJSONType(t, resp)
		assertErrorBody(t, b)
	}
}

func TestLargeBodyGetsFullResponse(t *testing.T) {
	ts := newTestServer(t)
	big := bytes.Repeat([]byte("x"), 2<<20)
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		resp, b := do(t, m, ts.URL+"/health", bytes.NewReader(big))
		if resp.StatusCode >= 500 {
			t.Errorf("%s /health with 2 MB body: status %d", m, resp.StatusCode)
		}
		assertJSONType(t, resp)
		if !json.Valid(b) {
			t.Errorf("%s /health with 2 MB body: invalid JSON %q", m, b)
		}
	}
}

func TestConcurrentHealth(t *testing.T) {
	ts := newTestServer(t)
	var wg sync.WaitGroup
	errs := make(chan string, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(ts.URL + "/health")
			if err != nil {
				errs <- err.Error()
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- resp.Status
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestAllowHeader(t *testing.T) {
	if got := allowHeader(methodSet{http.MethodGet: nil}); got != "GET, HEAD" {
		t.Errorf("allowHeader(GET) = %q", got)
	}
	if got := allowHeader(methodSet{http.MethodPost: nil}); got != "POST" {
		t.Errorf("allowHeader(POST) = %q", got)
	}
}

func TestOpenDBFreshRemovesOldData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	db, err := openDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items (name, stock, available) VALUES ('a', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = openDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("fresh database has %d items, want 0", n)
	}
}
