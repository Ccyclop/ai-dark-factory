package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// unknownPaths are well-formed request targets that are not a route in
// CONTRACT.md section 4. C-ERR-404: each returns 404 with C-REP-ERR.
//
// `/items`, `/items/1`, `/reservations`, `/reservations/1` are routes of the
// finished API but are not built yet in WI-1, and S-10's "Unknown ids return
// 404" covers them until they are.
var unknownPaths = []string{
	"/",
	"/nope",
	"/nope/",
	"/nope?x=1",
	"/health/",
	"/health/extra",
	"/health.json",
	"/Health",
	"/HEALTH",
	"/healthx",
	"/healthz",
	"/healths",
	"//health",
	"/./health",
	"/../health",
	"/health/../health",
	"/api/health",
	"/v1/health",
	"/items",
	"/items/",
	"/items/1",
	"/items/abc",
	"/reservations",
	"/reservations/",
	"/reservations/1",
	"/status",
	"/healthcheck",
	"/%2e%2e/health",
	"/a/b/c/d/e",
	strings.Repeat("/a", 100),
	"/nope?x=" + strings.Repeat("y", 4096),
}

func TestC_ERR_404_unknown_paths_are_404_json(t *testing.T) {
	for _, target := range unknownPaths {
		t.Run(target, func(t *testing.T) {
			r := do(t, http.MethodGet, target, nil, nil)
			mustStatus(t, r, http.StatusNotFound)
			mustContentTypeJSON(t, r)
			mustErrorBody(t, r)
		})
	}
}

// C-ERR-404: the 404 answer is the same shape for every unknown path, whatever
// the method, and never a 405 or a 5xx.
func TestC_ERR_404_unknown_paths_ignore_the_method(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, m := range methods {
		r := do(t, m, "/nope", []byte("{}"), nil)
		mustStatus(t, r, http.StatusNotFound)
		mustContentTypeJSON(t, r)
		mustErrorBody(t, r)
	}
}

// C-ERR-404 / C-ERR-500: a path far longer than any route is still an unknown
// path, answered completely and without a 5xx. 8000 characters is past the usual
// limits of proxies and servers and well past every route in the contract.
func TestC_ERR_404_very_long_path(t *testing.T) {
	for _, n := range []int{512, 2048, 8000} {
		t.Run("len-"+strconv.Itoa(n), func(t *testing.T) {
			target := "/" + strings.Repeat("x", n)
			r := do(t, http.MethodGet, target, nil, nil)
			mustStatus(t, r, http.StatusNotFound)
			mustContentTypeJSON(t, r)
			mustErrorBody(t, r)
			assertStillHealthy(t)
		})
	}
}

// C-ERR-404: the error body of an unknown path is exactly {"error": "<non-empty
// string>"} — one field, and never a bare string, an array, an empty object or
// an empty message.
func TestC_ERR_404_error_body_shape(t *testing.T) {
	r := do(t, http.MethodGet, "/definitely-not-a-route", nil, nil)
	mustStatus(t, r, http.StatusNotFound)
	msg := mustErrorBody(t, r)
	if strings.TrimSpace(msg) == "" {
		t.Fatalf("error message is blank: %q", msg)
	}
	if len(r.Body) == 0 {
		t.Fatal("404 with an empty body")
	}
	// The body is a JSON object, not a JSON string holding one.
	if r.Body[0] != '{' {
		t.Fatalf("404 body does not start with '{': %q", r.Body)
	}
}

// C-ERR-404: the 404 body is not an error code the contract assigns elsewhere.
// The contract fixes only the shape of the message, never its wording, so the
// test only insists the status is 404 and the shape is right.
func TestC_ERR_404_repeated_unknown_paths_are_stable(t *testing.T) {
	var first []byte
	for i := range 5 {
		r := do(t, http.MethodGet, "/still-not-a-route", nil, nil)
		mustStatus(t, r, http.StatusNotFound)
		mustErrorBody(t, r)
		if i == 0 {
			first = r.Body
		} else if string(r.Body) != string(first) {
			t.Fatalf("repeat %d: body %q, want %q", i, r.Body, first)
		}
	}
}
