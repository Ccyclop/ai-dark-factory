package api

import (
	"net/http"
	"testing"
)

// C-PATH-1 (D-26): how request paths are matched. The path as sent (still
// percent-encoded) is split on `/`, each segment is percent-decoded on its
// own, and the segments are compared exactly. A percent-encoded unreserved
// character is the same character (RFC 3986 §2.3, §6.2.2.2), so `/%68ealth`
// matches `/health`. `%2F` is decoded inside its segment and never acts as a
// separator. There is no other normalisation: a trailing slash, an empty
// segment or a dot-segment does not match. The query string is ignored when
// matching.
//
// These are the cases the /health route already fixes. The item and
// reservation cases arrive with WI-2 and extend this file.
func TestC_PATH_1_health_path_matching(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   int
	}{
		// A percent-encoded unreserved character is the same character:
		// /%68ealth decodes to /health and matches the route.
		{"encoded-unreserved", "/%68ealth", http.StatusOK},
		// Decoding keeps ASCII case: /%48ealth is /Health, which is not the
		// /health route.
		{"encoded-unreserved-case", "/%48ealth", http.StatusNotFound},
		// %2F is decoded inside its segment and never acts as a separator,
		// so /health%2Ffoo is the single segment "health/foo", not /health.
		{"encoded-slash", "/health%2Ffoo", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Raw bytes, not the net/http client: the client would decode
			// %68ealth to /health before sending and hide the server's own
			// decoding. The raw request line carries the path exactly as the
			// contract reads it.
			r := doRaw(t, rawReq{Method: http.MethodGet, Target: c.target})
			mustStatus(t, r, c.want)
			if c.want == http.StatusOK {
				mustHealthBody(t, r)
			} else {
				mustErrorBody(t, r)
			}
		})
	}
	assertStillHealthy(t)
}
