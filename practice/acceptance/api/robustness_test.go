package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// C-ERR-500: "no request, however malformed, oversized or concurrent, gets any
// 5xx status, and every request receives a complete HTTP response (no dropped
// connection)". Every test in this file sends something a server may legitimately
// refuse, insists on a complete non-5xx answer or an honest closed connection,
// and then proves the service is still serving.

// C-ERR-500: a request line that no HTTP parser can read.
func TestC_ERR_500_malformed_request_lines(t *testing.T) {
	for _, c := range malformedLineCases {
		t.Run(c.name, func(t *testing.T) {
			r, err := exchange(t, c.rr)
			t.Logf("%s: %s", c.name, mustSurvived(t, r, err, c.name))
			assertStillHealthy(t)
		})
	}
}

// C-ERR-500: header blocks past what the server will read. C-ERR-400 caps the
// request *body* at 1048576 bytes; nothing in the contract caps headers, so all
// the contract asks is no 5xx and a complete answer or an honest close.
func TestC_ERR_500_oversized_headers(t *testing.T) {
	cases := []struct {
		name string
		rr   rawReq
	}{
		{"single-2mb-header", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"X-Big: " + strings.Repeat("a", 2*1024*1024)}}},
		{"single-1mb-header", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"X-Big: " + strings.Repeat("a", 1024*1024)}}},
		{"long-header-value", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"X-Long: " + strings.Repeat("y", 70000)}}},
		{"many-headers", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: manyHeaders(2000, "X-Pad")}},
		{"huge-uri", rawReq{Method: http.MethodGet, Target: "/health?x=" + strings.Repeat("q", 64*1024)}},
		{"huge-cookie", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Cookie: " + strings.Repeat("c=1; ", 100000)}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := exchange(t, c.rr)
			t.Logf("%s: %s", c.name, mustSurvived(t, r, err, c.name))
			assertStillHealthy(t)
		})
	}
}

// C-ERR-500: request bodies far past the contract's 1048576-byte cap, on the
// route that serves them and on one that does not.
func TestC_ERR_500_oversized_bodies(t *testing.T) {
	for _, size := range []int{1048576, 1048577, 4 * 1024 * 1024} {
		body := []byte(strings.Repeat("a", size))
		t.Run("get-"+strconv.Itoa(size), func(t *testing.T) {
			r := do(t, http.MethodGet, "/health", body, nil)
			mustStatus(t, r, http.StatusOK)
			mustHealthBody(t, r)
		})
		t.Run("post-"+strconv.Itoa(size), func(t *testing.T) {
			r := do(t, http.MethodPost, "/health", body, nil)
			mustStatus(t, r, http.StatusMethodNotAllowed)
			mustErrorBody(t, r)
		})
		t.Run("unknown-path-"+strconv.Itoa(size), func(t *testing.T) {
			r := do(t, http.MethodPost, "/nope", body, nil)
			mustStatus(t, r, http.StatusNotFound)
			mustErrorBody(t, r)
		})
	}
	assertStillHealthy(t)
}

// C-ERR-500: bodies that end early, arrive unannounced, or contradict the
// headers.
func TestC_ERR_500_broken_framing(t *testing.T) {
	cases := []struct {
		name string
		rr   rawReq
	}{
		{"unannounced-body", rawReq{Method: http.MethodGet, Target: "/health",
			Body: []byte("surprise"), NoCL: true}},
		{"truncated-body", rawReq{Method: http.MethodPost, Target: "/health",
			Headers: []string{"Content-Length: 1000000"}, Body: []byte("only ten..")}},
		{"negative-content-length", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Content-Length: -1"}}},
		{"non-numeric-content-length", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Content-Length: seven"}}},
		{"two-content-lengths", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Content-Length: 5", "Content-Length: 6"}, Body: []byte("abcde")}},
		{"cl-and-chunked", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Content-Length: 5", "Transfer-Encoding: chunked"},
			Body:    []byte("0\r\n\r\n")}},
		{"unterminated-chunked", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Transfer-Encoding: chunked"}, Body: []byte("3\r\nabc\r\n")}},
		{"bad-chunk-size", rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Transfer-Encoding: chunked"}, Body: []byte("zz\r\nabc\r\n0\r\n\r\n")}},
		{"expect-100-but-no-body", rawReq{Method: http.MethodPost, Target: "/health",
			Headers: []string{"Expect: 100-continue", "Content-Length: 1048576"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := exchange(t, c.rr)
			t.Logf("%s: %s", c.name, mustSurvived(t, r, err, c.name))
			assertStillHealthy(t)
		})
	}
}

// C-ERR-500: targets that are not the exact path, including the ones an HTTP
// proxy would rewrite on the way in.
func TestC_ERR_500_unusual_targets(t *testing.T) {
	host := baseURL(t).Host
	cases := []struct {
		name string
		rr   rawReq
	}{
		{"absolute-form-own-host", rawReq{Method: http.MethodGet,
			Target: "http://" + host + "/health"}},
		{"absolute-form-other-host", rawReq{Method: http.MethodGet,
			Target: "http://elsewhere.invalid/health"}},
		{"absolute-form-no-path", rawReq{Method: http.MethodGet, Target: "http://" + host}},
		{"encoded-path", rawReq{Method: http.MethodGet, Target: "/%68ealth"}},
		{"double-encoded-path", rawReq{Method: http.MethodGet, Target: "/%2568ealth"}},
		{"bad-escape", rawReq{Method: http.MethodGet, Target: "/health%zz"}},
		{"nul-in-path", rawReq{Method: http.MethodGet, Target: "/health%00"}},
		{"dot-segments", rawReq{Method: http.MethodGet, Target: "/x/../../health"}},
		{"tab-in-path", rawReq{Method: http.MethodGet, Target: "/hea%09lth"}},
		{"semicolon", rawReq{Method: http.MethodGet, Target: "/health;x=1"}},
		{"connect-authority", rawReq{Method: http.MethodConnect, Target: host}},
		{"connect-path", rawReq{Method: http.MethodConnect, Target: "/health"}},
		{"options-star", rawReq{Method: http.MethodOptions, Target: "*"}},
		{"get-star", rawReq{Method: http.MethodGet, Target: "*"}},
		{"http10", rawReq{Method: http.MethodGet, Target: "/health", Proto: "HTTP/1.0", Close: true}},
		{"http09-style", rawReq{Prefix: []byte("GET /health\r\n\r\n")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := exchange(t, c.rr)
			t.Logf("%s: %s", c.name, mustSurvived(t, r, err, c.name))
			assertStillHealthy(t)
		})
	}
}

// C-ERR-404 / C-OP-HEALTH: the well-formed absolute form of the health route is
// still /health and still answers the health way.
func TestC_OP_HEALTH_absolute_form_target(t *testing.T) {
	r := doRaw(t, rawReq{Method: http.MethodGet, Target: "http://" + baseURL(t).Host + "/health"})
	mustHealthBody(t, r)
}

// C-ERR-500: two requests on one connection both get complete answers, so the
// connection stays usable.
func TestC_ERR_500_pipelined_requests(t *testing.T) {
	rs := pipeline(t, []rawReq{
		{Method: http.MethodGet, Target: "/health"},
		{Method: http.MethodPost, Target: "/health"},
		{Method: http.MethodGet, Target: "/nope"},
		{Method: http.MethodGet, Target: "/health"},
	})
	if len(rs) != 4 {
		t.Fatalf("got %d responses, want 4", len(rs))
	}
	mustStatus(t, rs[0], http.StatusOK)
	mustHealthBody(t, rs[0])
	mustStatus(t, rs[1], http.StatusMethodNotAllowed)
	mustErrorBody(t, rs[1])
	mustStatus(t, rs[2], http.StatusNotFound)
	mustErrorBody(t, rs[2])
	mustStatus(t, rs[3], http.StatusOK)
	mustHealthBody(t, rs[3])
}

// C-ERR-500: connections opened and closed without a request, and requests whose
// headers are nonsense, leave the service serving.
func TestC_ERR_500_idle_and_broken_connections(t *testing.T) {
	for range 100 {
		c := dial(t)
		if err := c.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	cases := []rawReq{
		{Method: http.MethodGet, Target: "/health", Headers: []string{"Host: localhost:8080",
			"X: " + strings.Repeat("v", 1000)}},
		{Method: http.MethodGet, Target: "/health", Headers: []string{"Content-Length: 0",
			"Content-Type: " + strings.Repeat("t", 500)}},
		{Method: http.MethodGet, Target: "/health", Headers: []string{"Connection: upgrade",
			"Upgrade: h2c"}},
		{Method: http.MethodGet, Target: "/health", Headers: []string{"Idempotency-Key: " +
			strings.Repeat("k", 1000)}},
	}
	for i, rr := range cases {
		r, err := exchange(t, rr)
		t.Logf("case %d: %s", i, mustSurvived(t, r, err, "broken connection case"))
	}
	assertStillHealthy(t)
}

func manyHeaders(n int, name string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = name + "-Pad: " + strings.Repeat("p", 512)
	}
	return out
}

func bytesRepeat(c byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return b
}
