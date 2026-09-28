package api

import (
	"net/http"
	"strings"
	"testing"
)

// C-ERR-405: a route path used with a method it does not support returns 405
// with C-REP-ERR and an Allow header. C-OP-HEALTH is GET, so every other method
// token is a method /health does not support.
func TestC_ERR_405_wrong_method_on_health(t *testing.T) {
	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodTrace,
		http.MethodOptions,
		"FOO", // a method token the contract never names
		"get", // method tokens are case-sensitive: lower case is not GET
		"PROPFIND",
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			r := do(t, m, "/health", nil, nil)
			mustStatus(t, r, http.StatusMethodNotAllowed)
			mustContentTypeJSON(t, r)
			mustErrorBody(t, r)
			mustAllowHeader(t, r)
		})
	}
}

// C-ERR-405: a wrong method with a body is still a wrong method, and the
// response is complete: the client is not left waiting on an unread request.
func TestC_ERR_405_wrong_method_with_body(t *testing.T) {
	bodies := [][]byte{
		nil,
		{},
		[]byte("{}"),
		[]byte(`{"name":"widget","stock":3}`),
		[]byte(strings.Repeat("z", 4000)),
		[]byte("not json at all"),
		[]byte("\x00\x01\x02\xff"),
	}
	for i, b := range bodies {
		r := do(t, http.MethodPost, "/health", b, http.Header{"Content-Type": []string{"application/json"}})
		mustStatus(t, r, http.StatusMethodNotAllowed)
		mustContentTypeJSON(t, r)
		mustErrorBody(t, r)
		mustAllowHeader(t, r)
		if len(b) > 0 && len(r.Body) == 0 {
			t.Fatalf("body %d: empty 405 answer", i)
		}
	}
	assertStillHealthy(t)
}

// C-ERR-405 / C-ERR-500: a large body on a wrong method gets a complete 405
// answer, not a reset connection and not a 5xx. C-ERR-400's oversized-body rule
// belongs to the operations of section 4; a method the route does not serve has
// no body to validate.
func TestC_ERR_405_wrong_method_with_large_body(t *testing.T) {
	for _, size := range []int{1024, 65536, 1048577, 2 * 1048576} {
		r := do(t, http.MethodPost, "/health", []byte(strings.Repeat("a", size)), nil)
		mustStatus(t, r, http.StatusMethodNotAllowed)
		mustContentTypeJSON(t, r)
		mustErrorBody(t, r)
		mustAllowHeader(t, r)
	}
	assertStillHealthy(t)
}

// C-ERR-405: the 405 answer is stable when the same wrong request is repeated,
// and a wrong method on an unknown path is still the 404 of C-ERR-404, not a
// 405: an unrouted path has no method to support.
func TestC_ERR_405_repeated_and_unknown_path_still_404(t *testing.T) {
	var first result
	for i := range 5 {
		r := do(t, http.MethodPut, "/health", nil, nil)
		mustStatus(t, r, http.StatusMethodNotAllowed)
		mustAllowHeader(t, r)
		if i == 0 {
			first = r
			continue
		}
		if string(r.Body) != string(first.Body) {
			t.Fatalf("repeat %d: body %q, want %q", i, r.Body, first.Body)
		}
		if r.Header.Get("Allow") != first.Header.Get("Allow") {
			t.Fatalf("repeat %d: Allow %q, want %q", i, r.Header.Get("Allow"), first.Header.Get("Allow"))
		}
	}
	r := do(t, http.MethodPut, "/nope", nil, nil)
	mustStatus(t, r, http.StatusNotFound)
	mustErrorBody(t, r)
}
