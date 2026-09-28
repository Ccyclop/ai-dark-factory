// Package api is the black-box acceptance suite for the practice inventory
// reservation service (CONTRACT.md, planner @0d3f957).
//
// Every test in this package talks to a running instance over HTTP only: no
// package of the service is imported, no file of it is read, no internal state is
// inspected. The service is started by ../run.sh with the exact command
// C-RUN-1 requires, so the suite runs from a sidecar container attached to that
// container's network namespace (PRACTICE_BASE_URL defaults to
// http://localhost:8080, which inside that namespace is the service).
package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The service runs under --cpus=1 --memory=512m, so every client timeout here is
// generous: a slow answer must never be mistaken for a broken service.
const requestTimeout = 30 * time.Second

// defaultBaseURL is C-RUN-2's port on loopback, which is the service from inside
// the container network namespace the suite is attached to.
const defaultBaseURL = "http://localhost:8080"

// baseURLFromEnv is baseURL without a *testing.T, for the goroutine helpers.
func baseURLFromEnv() *url.URL {
	raw := os.Getenv("PRACTICE_BASE_URL")
	if raw == "" {
		raw = defaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{Scheme: "http", Host: "localhost:8080"}
	}
	return u
}

func baseURL(t *testing.T) *url.URL {
	t.Helper()
	raw := os.Getenv("PRACTICE_BASE_URL")
	if raw == "" {
		raw = defaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("PRACTICE_BASE_URL=%q is not a URL: %v", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		t.Fatalf("PRACTICE_BASE_URL=%q must be an absolute URL such as %s", raw, defaultBaseURL)
	}
	return u
}

func newClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 256,
			MaxConnsPerHost:     256,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
		},
		// A redirect is a routing answer like any other and must be observed,
		// not followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// result is one complete HTTP response: status, headers and the full body.
type result struct {
	Method string
	Target string
	Status int
	Proto  string
	Header http.Header
	Body   []byte
}

func (r result) String() string {
	return fmt.Sprintf("%s %s -> %d %s, %d body bytes, Content-Type %q",
		r.Method, r.Target, r.Status, r.Proto, len(r.Body), r.Header.Get("Content-Type"))
}

// do sends one request through net/http and reads the complete response.
func do(t *testing.T, method, target string, body []byte, header http.Header) result {
	t.Helper()
	r, err := doWith(newClient(), method, target, body, header)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	return r
}

// doWith is do for the cases that run in goroutines, where t.Fatalf is illegal.
func doWith(c *http.Client, method, target string, body []byte, header http.Header) (result, error) {
	u, err := url.Parse(target)
	if err != nil {
		return result{}, fmt.Errorf("target %q: %w", target, err)
	}
	base := baseURLFromEnv()
	base.Path = u.Path
	base.RawQuery = u.RawQuery
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, base.String(), rdr)
	if err != nil {
		return result{}, fmt.Errorf("build %s %s: %w", method, target, err)
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		return result{}, fmt.Errorf("%s %s: %w", method, target, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return result{}, fmt.Errorf("%s %s: incomplete body: %w", method, target, err)
	}
	return result{Method: method, Target: target, Status: resp.StatusCode, Proto: resp.Proto,
		Header: resp.Header, Body: raw}, nil
}

// rawReq is a request written onto the socket byte by byte, so that a test can
// send a target or a header that net/http would refuse to build.
type rawReq struct {
	Method  string
	Target  string
	Proto   string // default HTTP/1.1
	Headers []string
	Body    []byte
	NoCL    bool // send a body without announcing Content-Length
	NoHost  bool
	Close   bool // send Connection: close
	Prefix  []byte
	Suffix  []byte
}

func (rr rawReq) bytes(t *testing.T) []byte {
	t.Helper()
	proto := rr.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}
	target := rr.Target
	if target == "" {
		target = "/"
	}
	var b bytes.Buffer
	if rr.Prefix != nil {
		b.Write(rr.Prefix)
	}
	fmt.Fprintf(&b, "%s %s %s\r\n", rr.Method, target, proto)
	if !rr.NoHost {
		fmt.Fprintf(&b, "Host: %s\r\n", baseURL(t).Host)
	}
	for _, h := range rr.Headers {
		b.WriteString(h)
		b.WriteString("\r\n")
	}
	if rr.Body != nil && !rr.NoCL {
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(rr.Body))
	}
	if rr.Close {
		b.WriteString("Connection: close\r\n")
	}
	b.WriteString("\r\n")
	b.Write(rr.Body)
	if rr.Suffix != nil {
		b.Write(rr.Suffix)
	}
	return b.Bytes()
}

// dialRaw is dial for the cases that run in goroutines, where t.Fatalf is illegal.
func dialRaw() (net.Conn, error) {
	u := baseURLFromEnv()
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", u, err)
	}
	if err := c.SetDeadline(time.Now().Add(requestTimeout)); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func dial(t *testing.T) net.Conn {
	t.Helper()
	c, err := dialRaw()
	if err != nil {
		t.Fatalf("%v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// noResponseError says the service closed the connection without a status line.
// A request can be malformed at the protocol level, before any route exists to
// answer it, and a closed connection is the honest reply to that.
type noResponseError struct{ err error }

func (e *noResponseError) Error() string { return e.err.Error() }
func (e *noResponseError) Unwrap() error { return e.err }

// readOne parses one response and insists on a complete one: a truncated body
// (a short read against Content-Length, a cut chunked stream) is an error here,
// which is exactly what C-ERR-500 forbids.
func readOne(c net.Conn, method string) (result, error) {
	resp, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: method})
	if err != nil {
		return result{}, &noResponseError{err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return result{Status: resp.StatusCode, Proto: resp.Proto, Header: resp.Header},
			fmt.Errorf("incomplete body: %w", err)
	}
	return result{Status: resp.StatusCode, Proto: resp.Proto, Header: resp.Header, Body: body}, nil
}

// exchange writes rawReq and returns the response. A non-nil error means the
// service answered nothing at all (the connection was closed without a status
// line), which some malformed requests legitimately earn. The write side is
// half-closed after the request, so a service still waiting for the rest of a
// body sees EOF instead of hanging until the test times out.
func exchange(t *testing.T, rr rawReq) (result, error) {
	t.Helper()
	c := dial(t)
	if _, err := c.Write(rr.bytes(t)); err != nil {
		return result{}, fmt.Errorf("write: %w", err)
	}
	if tcp, ok := c.(*net.TCPConn); ok {
		if err := tcp.CloseWrite(); err != nil {
			return result{}, fmt.Errorf("close write side: %w", err)
		}
	}
	res, err := readOne(c, rr.Method)
	if err != nil {
		return result{}, err
	}
	res.Method, res.Target = rr.Method, rr.Target
	return res, nil
}

// doRaw is exchange for cases where a response is required.
func doRaw(t *testing.T, rr rawReq) result {
	t.Helper()
	res, err := exchange(t, rr)
	if err != nil {
		t.Fatalf("%s %s: no complete response: %v", rr.Method, rr.Target, err)
	}
	return res
}

// noResponse writes bytes that no conforming server can parse and returns
// whatever came back.
func rawBytes(t *testing.T, rr rawReq) (result, error) { return exchange(t, rr) }

// pipeline writes several requests on one connection and reads every response,
// which also proves the service keeps the connection usable.
func pipeline(t *testing.T, reqs []rawReq) []result {
	t.Helper()
	c := dial(t)
	var out []result
	br := bufio.NewReader(c)
	for i, rr := range reqs {
		if _, err := c.Write(rr.bytes(t)); err != nil {
			t.Fatalf("request %d: write: %v", i, err)
		}
		// Read one response per request off the same buffered reader.
		resp, err := http.ReadResponse(br, &http.Request{Method: rr.Method})
		if err != nil {
			t.Fatalf("request %d (%s %s): no complete response: %v", i, rr.Method, rr.Target, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("request %d (%s %s): incomplete body: %v", i, rr.Method, rr.Target, err)
		}
		out = append(out, result{Method: rr.Method, Target: rr.Target, Status: resp.StatusCode,
			Proto: resp.Proto, Header: resp.Header, Body: body})
	}
	return out
}

// ---- assertions ----------------------------------------------------------

func mustStatus(t *testing.T, r result, want int) {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status: got %d, want %d (%s)\nbody: %s", r.Status, want, r, r.Body)
	}
}

// mustNoServerError is C-ERR-500: no request, however malformed, gets a 5xx.
func mustNoServerError(t *testing.T, r result, what string) {
	t.Helper()
	if r.Status >= 500 || r.Status == 0 {
		t.Fatalf("%s: got status %d, want no 5xx (%s)\nbody: %s", what, r.Status, r, r.Body)
	}
}

// mustSurvived is C-ERR-500 for a request the service is allowed to refuse at the
// protocol level: either a complete response, or no response at all with the
// connection closed. D-27 allows any 5xx for such a request except 500, because
// the only 5xx that can appear here are net/http's own, sent before any handler
// runs, as its fixed standard-library behaviour (501 for an unsupported
// Transfer-Encoding, 505 for an HTTP major version other than 1, and the like).
// 500 stays forbidden for every request, as the brief says. An incomplete
// response is a failure, because C-ERR-500 also demands a complete answer. The
// status is returned so the test log records what the service actually said.
func mustSurvived(t *testing.T, r result, err error, what string) string {
	t.Helper()
	var none *noResponseError
	switch {
	case errors.As(err, &none):
		return "closed without a response"
	case err != nil:
		t.Fatalf("%s: %v", what, err)
	}
	if r.Status == 0 || r.Status == 500 {
		t.Fatalf("%s: got status %d, want never 500 (%s)\nbody: %s", what, r.Status, r, r.Body)
	}
	return strconv.Itoa(r.Status)
}

// mustContentTypeJSON is C-REP-HDR. Only the media type is contracted, so
// parameters such as charset are accepted.
func mustContentTypeJSON(t *testing.T, r result) {
	t.Helper()
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		t.Fatalf("Content-Type: missing (%s)", r)
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("Content-Type %q: %v (%s)", ct, err, r)
	}
	if mt != "application/json" {
		t.Fatalf("Content-Type: got %q, want application/json (%s)", ct, r)
	}
}

// mustSingleJSONObject is C-REP-HDR: the body is a single JSON object, with
// nothing before or after it.
func mustSingleJSONObject(t *testing.T, r result) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("body is not a JSON object: %v (%s)\nbody: %q", err, r, r.Body)
	}
	if obj == nil {
		t.Fatalf("body is JSON null, not an object (%s)\nbody: %q", r, r.Body)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("body carries more than one JSON value: err=%v (%s)\nbody: %q", err, r, r.Body)
	}
	return obj
}

// mustErrorBody is C-REP-ERR: exactly one field, "error", a non-empty string.
func mustErrorBody(t *testing.T, r result) string {
	t.Helper()
	obj := mustSingleJSONObject(t, r)
	if len(obj) != 1 {
		t.Fatalf("error body has %d fields, want exactly 1 (%s)\nbody: %q", len(obj), r, r.Body)
	}
	msg, ok := obj["error"]
	if !ok {
		t.Fatalf(`error body has no "error" field (%s)`, r)
	}
	s, ok := msg.(string)
	if !ok {
		t.Fatalf(`"error" is %T, want a JSON string (%s)`, msg, r)
	}
	if s == "" {
		t.Fatalf(`"error" is empty, want a non-empty message (%s)`, r)
	}
	return s
}

// mustHealth is C-OP-HEALTH, used both as an assertion and as the liveness probe.
func mustHealth(t *testing.T, r result) {
	t.Helper()
	mustStatus(t, r, http.StatusOK)
	mustHealthBody(t, r)
}

// mustHealthBody is the body half of C-OP-HEALTH, for answers that are health
// bodies but not necessarily 200 (a wrong method on /health, for instance).
func mustHealthBody(t *testing.T, r result) {
	t.Helper()
	mustContentTypeJSON(t, r)
	obj := mustSingleJSONObject(t, r)
	if len(obj) != 1 {
		t.Fatalf("health body has %d fields, want exactly 1: \"status\" (%s)\nbody: %q", len(obj), r, r.Body)
	}
	if v, ok := obj["status"]; !ok || v != "ok" {
		t.Fatalf(`health body: got %q, want {"status":"ok"} (%s)`, r.Body, r)
	}
}

// assertStillHealthy is C-ERR-500's "no dropped connection, still serving":
// every abuse test ends by proving the service is intact.
func assertStillHealthy(t *testing.T) {
	t.Helper()
	mustHealth(t, do(t, http.MethodGet, "/health", nil, nil))
}

// mustAllowHeader is C-ERR-405: a 405 carries an Allow header. Only the presence
// of the header is contracted; the test also insists it names GET, the method
// the route does support, and lists nothing the route does not serve.
func mustAllowHeader(t *testing.T, r result) {
	t.Helper()
	allow := r.Header.Get("Allow")
	if strings.TrimSpace(allow) == "" {
		t.Fatalf("405 without an Allow header (%s)", r)
	}
	var methods []string
	for _, m := range strings.Split(allow, ",") {
		if m = strings.TrimSpace(m); m != "" {
			methods = append(methods, m)
		}
	}
	if len(methods) == 0 {
		t.Fatalf("405 with an empty Allow header: %q (%s)", allow, r)
	}
	found := false
	for _, m := range methods {
		if m == http.MethodGet {
			found = true
		}
		if m == http.MethodPost || m == http.MethodPut || m == http.MethodDelete || m == http.MethodPatch {
			t.Fatalf("405 Allow header lists %q, which /health does not serve: %q", m, allow)
		}
	}
	if !found {
		t.Fatalf("405 Allow header %q does not list GET", allow)
	}
}
