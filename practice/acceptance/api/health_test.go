package api

import (
	"net/http"
	"testing"
)

// C-OP-HEALTH: `GET /health` → 200, body {"status": "ok"} (D-12).
func TestC_OP_HEALTH_get_health(t *testing.T) {
	mustHealth(t, do(t, http.MethodGet, "/health", nil, nil))
}

// C-OP-HEALTH with a query string: the path is still the exact path /health, the
// query is not part of it, so the answer is the health answer.
func TestC_OP_HEALTH_query_string_is_not_the_path(t *testing.T) {
	for _, target := range []string{"/health?", "/health?x=1", "/health?a=1&a=2&b=%20", "/health?%zz"} {
		r := do(t, http.MethodGet, target, nil, nil)
		mustStatus(t, r, http.StatusOK)
		mustContentTypeJSON(t, r)
		obj := mustSingleJSONObject(t, r)
		if len(obj) != 1 || obj["status"] != "ok" {
			t.Fatalf("%s: got %q, want {\"status\":\"ok\"}", target, r.Body)
		}
	}
}

// C-OP-HEALTH: the same request repeated is the same answer, on a fresh
// connection every time, with no state to carry.
func TestC_OP_HEALTH_repeated_identical_requests(t *testing.T) {
	var first []byte
	for i := range 20 {
		r := do(t, http.MethodGet, "/health", nil, nil)
		mustHealth(t, r)
		if i == 0 {
			first = r.Body
			continue
		}
		if string(r.Body) != string(first) {
			t.Fatalf("repeat %d: body %q, want %q", i, r.Body, first)
		}
	}
}

// C-REP-HDR: the body is a single JSON object, nothing before or after it. The
// health body is tiny, so a stray byte would be obvious.
func TestC_REP_HDR_health_body_is_one_json_object(t *testing.T) {
	r := do(t, http.MethodGet, "/health", nil, nil)
	if len(r.Body) == 0 {
		t.Fatalf("empty body (%s)", r)
	}
	if r.Body[0] != '{' || r.Body[len(r.Body)-1] != '}' {
		t.Fatalf("body is not a single JSON object: %q", r.Body)
	}
	mustSingleJSONObject(t, r)
}

// C-REP-HDR: a response to HEAD carries no body, and still carries
// Content-Type: application/json.
func TestC_REP_HDR_head_health_has_no_body(t *testing.T) {
	r := do(t, http.MethodHead, "/health", nil, nil)
	mustStatus(t, r, http.StatusOK)
	mustContentTypeJSON(t, r)
	if len(r.Body) != 0 {
		t.Fatalf("HEAD /health returned %d body bytes, want 0: %q", len(r.Body), r.Body)
	}
}

// C-REP-HDR: the answer does not depend on the client's framing. A health
// request with a body, with a body it never announces, and with a duplicated
// header, all address the same route and must be answered completely.
func TestC_REP_HDR_health_ignores_request_framing(t *testing.T) {
	cases := []struct {
		name string
		rr   rawReq
	}{
		{"body", rawReq{Method: http.MethodGet, Target: "/health", Body: []byte(`{"noise":true}`)}},
		{"body with content-type", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Content-Type: application/json"}, Body: []byte(`{"name":"x","stock":1}`)}},
		{"chunked body", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Transfer-Encoding: chunked"}, Body: []byte("3\r\nabc\r\n0\r\n\r\n")}},
		{"duplicate headers", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"X-Probe: a", "X-Probe: b", "Accept: */*", "Accept: application/json"}}},
		{"expect continue", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Expect: 100-continue"}, Body: []byte("unused")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := doRaw(t, c.rr)
			mustStatus(t, r, http.StatusOK)
			mustContentTypeJSON(t, r)
			obj := mustSingleJSONObject(t, r)
			if len(obj) != 1 || obj["status"] != "ok" {
				t.Fatalf("got %q, want {\"status\":\"ok\"}", r.Body)
			}
			assertStillHealthy(t)
		})
	}
}
