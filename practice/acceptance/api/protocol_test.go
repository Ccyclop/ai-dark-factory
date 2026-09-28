package api

import (
	"net/http"
	"strings"
	"testing"
)

// C-ERR-500, the entry that goes wrong most easily: "no request, however
// malformed, oversized or concurrent, gets any 5xx status, and every request
// receives a complete HTTP response (no dropped connection)".
//
// A standard library HTTP server answers some requests before any handler can see
// them, and for a few of those it picks a 5xx of its own: 501 for a transfer
// coding it does not implement (RFC 9112 §7.1.1 says a server SHOULD reply 501,
// and Go does), 505 for an HTTP version it does not implement. Both are
// malformed requests, so C-ERR-500 covers them and they are permanent acceptance
// cases.
//
// These tests assert the contract, not Go's habits, so they fail until the
// service answers these requests itself. Any status the contract allows (400,
// 404, 405, 414, 431, or an honest close with no answer at all) is accepted; a
// 5xx never is.

func TestC_ERR_500_unsupported_transfer_codings(t *testing.T) {
	for _, c := range transferCodingCases {
		t.Run(c, func(t *testing.T) {
			for _, withBody := range []bool{false, true} {
				rr := rawReq{Method: http.MethodPost, Target: "/health",
					Headers: []string{"Transfer-Encoding: " + c}}
				if withBody {
					rr.Body = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00")
				}
				r, err := exchange(t, rr)
				t.Logf("Transfer-Encoding: %-16s body=%-5v -> %s", c, withBody,
					mustSurvived(t, r, err, "Transfer-Encoding: "+c))
			}
			assertStillHealthy(t)
		})
	}
}

func TestC_ERR_500_unsupported_http_versions(t *testing.T) {
	for _, v := range httpVersionCases {
		t.Run(v, func(t *testing.T) {
			r, err := exchange(t, rawReq{Method: http.MethodGet, Target: "/health", Proto: v, Close: true})
			t.Logf("%-10s -> %s", v, mustSurvived(t, r, err, v))
			assertStillHealthy(t)
		})
	}
}

// C-ERR-500: the remaining shapes a server may refuse before routing.
func TestC_ERR_500_other_pre_routing_refusals(t *testing.T) {
	for _, c := range preRoutingCases {
		t.Run(c.name, func(t *testing.T) {
			r, err := exchange(t, c.rr)
			t.Logf("%s: %s", c.name, mustSurvived(t, r, err, c.name))
			assertStillHealthy(t)
		})
	}
}

// C-REP-HDR and C-ERR-500 as amended by D-25 and D-27: the JSON body and
// Content-Type rule, and the no-5xx rule, cover every request that Go's
// net/http server parses and passes to the handler. A response net/http
// produces by itself, before any handler runs, is excluded from both rules,
// whatever its status; the one exception is 500, which stays forbidden for
// every request. Known pre-handler responses are 400 (malformed request line
// or headers), 417 (unsupported Expect), 431 (headers too large), 501
// (unsupported Transfer-Encoding) and 505 (HTTP major version other than 1).
//
// This test pins both sides of that boundary. The handlerReachedNames cases are
// the ones net/http parses and passes to the handler — observed by their
// handler-produced 200/404/405 — so for them the JSON rule holds in full and no
// 5xx at all is allowed. Every other case in the lists below is refused by
// net/http before any handler runs, so only "never 500, complete answer" binds
// it.
var handlerReachedNames = map[string]bool{
	"version-HTTP/1.2":     true,
	"version-HTTP/1.9":     true,
	"request-line-16k":     true,
	"obs-fold":             true,
	"upgrade-h2c-settings": true,
	"te-chunked-trailer":   true,
	"bare-lf-headers":      true,
	"http2-preface":        true,
}

func TestC_REP_HDR_D25_json_rule_stops_at_the_handler(t *testing.T) {
	check := func(t *testing.T, name string, rr rawReq) {
		t.Helper()
		r, err := exchange(t, rr)
		if handlerReachedNames[name] {
			if err != nil {
				t.Fatalf("%s: no complete response: %v", name, err)
			}
			mustNoServerError(t, r, name)
			mustContentTypeJSON(t, r)
			if r.Status == http.StatusOK {
				mustHealthBody(t, r)
			} else {
				mustErrorBody(t, r)
			}
			return
		}
		t.Logf("%s: %s", name, mustSurvived(t, r, err, name))
	}
	for _, c := range transferCodingCases {
		t.Run("te-"+c, func(t *testing.T) {
			check(t, "te-"+c, rawReq{Method: http.MethodPost, Target: "/health",
				Headers: []string{"Transfer-Encoding: " + c}, Body: []byte("\x1f\x8b")})
		})
	}
	for _, v := range httpVersionCases {
		t.Run("version-"+v, func(t *testing.T) {
			check(t, "version-"+v, rawReq{Method: http.MethodGet, Target: "/health", Proto: v, Close: true})
		})
	}
	for _, c := range preRoutingCases {
		t.Run(c.name, func(t *testing.T) { check(t, c.name, c.rr) })
	}
	for _, c := range malformedLineCases {
		t.Run(c.name, func(t *testing.T) { check(t, c.name, c.rr) })
	}
}

// C-ERR-500: after every refusal above, the service is still serving the health
// route exactly as C-OP-HEALTH says.
func TestC_ERR_500_still_serving_after_every_refusal(t *testing.T) {
	for i := range 3 {
		r, err := exchange(t, rawReq{Method: http.MethodGet, Target: "/health",
			Headers: []string{"Transfer-Encoding: gzip"}, Body: []byte("\x1f\x8b"),
			Proto: "HTTP/9.9", Close: true})
		t.Logf("combined abuse %d: %s", i, mustSurvived(t, r, err, "combined abuse"))
	}
	mustHealth(t, do(t, http.MethodGet, "/health", nil, nil))
}

// transferCodingCases are Transfer-Encoding values no client may send to this
// API, and the ones Go accepts only to answer them itself.
var transferCodingCases = []string{
	"gzip",
	"deflate",
	"compress",
	"xchunked",
	"chunked, gzip",
	"gzip, chunked",
	"chunked, identity",
	"identity, chunked",
	"CHUNKED, GZIP",
	"br",
	"zstd",
}

// httpVersionCases are HTTP versions the service does not speak. Only 1.0 and 1.1
// are on offer (C-RUN-2 fixes a port, the contract fixes no version negotiation).
var httpVersionCases = []string{
	"HTTP/0.9", "HTTP/1.2", "HTTP/1.9", "HTTP/2.0", "HTTP/2.5", "HTTP/3.0",
	"HTTP/9.9", "HTTP/10.0", "HTTP/0.0",
}

// preRoutingCases are the remaining shapes a server may refuse before a route
// exists to answer them.
var preRoutingCases = []struct {
	name string
	rr   rawReq
}{
	{"request-line-16k", rawReq{Method: http.MethodGet,
		Target: "/" + strings.Repeat("q", 16000), Close: true}},
	{"host-header-16k", rawReq{Method: http.MethodGet, Target: "/health",
		Headers: []string{"Host: " + strings.Repeat("h", 16000)}}},
	{"content-length-overflow", rawReq{Method: http.MethodPost, Target: "/health",
		Headers: []string{"Content-Length: 99999999999999999999"}}},
	{"content-length-past-int64", rawReq{Method: http.MethodPost, Target: "/health",
		Headers: []string{"Content-Length: 9223372036854775808"}}},
	{"leading-zero-content-length", rawReq{Method: http.MethodPost, Target: "/health",
		Headers: []string{"Content-Length: 007"}, Body: []byte("abc")}},
	{"header-space-before-colon", rawReq{Method: http.MethodGet, Target: "/health",
		Headers: []string{"X-Pad : v"}}},
	{"obs-fold", rawReq{Method: http.MethodGet, Target: "/health",
		Headers: []string{"X-Pad: one\r\n\ttwo"}}},
	{"utf8-in-header-name", rawReq{Method: http.MethodGet, Target: "/health",
		Headers: []string{"X-Päd: v"}}},
	{"nul-in-header-value", rawReq{Method: http.MethodGet, Target: "/health",
		Headers: []string{"X-Pad: a\x00b"}}},
	{"upgrade-h2c-settings", rawReq{Method: http.MethodGet, Target: "/health",
		Headers: []string{"Upgrade: h2c", "HTTP2-Settings: AAMAAABkAAQAAP__"}}},
	{"te-chunked-trailer", rawReq{Method: http.MethodPost, Target: "/health",
		Headers: []string{"Transfer-Encoding: chunked", "Trailer: X-Sum"},
		Body:    []byte("0\r\nX-Sum: 1\r\n\r\n")}},
}

// malformedLineCases are request lines and header blocks that no HTTP parser can
// read.
var malformedLineCases = []struct {
	name string
	rr   rawReq
}{
	{"empty", rawReq{Prefix: []byte("\r\n\r\n")}},
	{"only-crlf", rawReq{Prefix: []byte("\r\n")}},
	{"no-version", rawReq{Prefix: []byte("GET /health\r\n\r\n")}},
	{"no-target", rawReq{Prefix: []byte("GET  HTTP/1.1\r\nHost: localhost\r\n\r\n")}},
	{"four-tokens", rawReq{Prefix: []byte("GET /health HTTP/1.1 extra\r\nHost: localhost\r\n\r\n")}},
	{"double-space", rawReq{Prefix: []byte("GET  /health  HTTP/1.1\r\nHost: localhost\r\n\r\n")}},
	{"version-only", rawReq{Prefix: []byte("GET /health HTTP/\r\n\r\n")}},
	{"no-method", rawReq{Prefix: []byte("/health HTTP/1.1\r\nHost: localhost\r\n\r\n")}},
	{"nul-in-line", rawReq{Prefix: []byte("GET /health\x00 HTTP/1.1\r\nHost: localhost\r\n\r\n")}},
	{"no-host-http11", rawReq{Prefix: []byte("GET /health HTTP/1.1\r\n\r\n")}},
	{"bad-header-line", rawReq{Prefix: []byte("GET /health HTTP/1.1\r\nHost: localhost\r\nnocolon\r\n\r\n")}},
	{"space-in-header-name", rawReq{Prefix: []byte("GET /health HTTP/1.1\r\nHost: localhost\r\nX Bad: v\r\n\r\n")}},
	{"bare-lf-headers", rawReq{Prefix: []byte("GET /health HTTP/1.1\nHost: localhost\n\n")}},
	{"http2-preface", rawReq{Prefix: []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")}},
	{"garbage", rawReq{Prefix: []byte("\x00\x01\x02\x03\x04\x05\x06\x07")}},
}
