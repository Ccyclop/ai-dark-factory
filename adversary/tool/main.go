// Command advtool is the adversary seat's black-box HTTP client.
//
// It runs inside the service's network namespace (C-RUN-4) and speaks only to
// http://127.0.0.1:8080. It exists because the attacks the mandate requires --
// simultaneity, byte-level malformed requests, oversized bodies, dropped
// connections -- cannot be expressed with a plain HTTP client.
//
// Every subcommand reports whether a *complete* HTTP response came back. A
// response is complete when a status line, a header block and the whole framed
// body were read. C-ERR-500 requires that of every request, so an incomplete
// read is a contract violation, not a client hiccup.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const addr = "127.0.0.1:8080"

var deadline = 30 * time.Second

// resp is one observed response.
type resp struct {
	complete  bool
	note      string
	status    int
	proto     string
	header    httpHeader
	body      []byte
	bodyTrunc bool
	duration  time.Duration
}

func (r resp) contentType() string { return r.header.get("Content-Type") }

// isSingleJSONObject reports whether body decodes to exactly one JSON object,
// which C-REP-HDR requires of every response body.
func (r resp) isSingleJSONObject() bool {
	dec := json.NewDecoder(bytes.NewReader(r.body))
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		return false
	}
	// Reject trailing content after the object.
	if _, err := dec.Token(); err != io.EOF {
		return false
	}
	return true
}

// httpHeader is a case-insensitive header map that keeps the raw first value.
type httpHeader map[string]string

func (h httpHeader) get(k string) string { return h[strings.ToLower(k)] }

func (h httpHeader) set(k, v string) { h[strings.ToLower(k)] = v }

// readResp reads one HTTP/1.x response with an explicit deadline.
func readResp(c net.Conn, lim time.Duration) resp {
	out := resp{header: httpHeader{}}
	_ = c.SetReadDeadline(time.Now().Add(lim))
	br := bufio.NewReaderSize(c, 64<<10)

	line, err := br.ReadString('\n')
	if err != nil {
		out.note = "no status line: " + err.Error()
		return out
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		out.note = "malformed status line: " + strconv.Quote(line)
		return out
	}
	out.proto = parts[0]
	out.status, _ = strconv.Atoi(parts[1])

	for {
		hl, err := br.ReadString('\n')
		if err != nil {
			out.note = "truncated headers: " + err.Error()
			return out
		}
		hl = strings.TrimRight(hl, "\r\n")
		if hl == "" {
			break
		}
		if i := strings.IndexByte(hl, ':'); i > 0 {
			out.header.set(strings.TrimSpace(hl[:i]), strings.TrimSpace(hl[i+1:]))
		}
	}

	// Body framing: chunked, Content-Length, or read to EOF.
	var body io.Reader = br
	if strings.EqualFold(out.header.get("Transfer-Encoding"), "chunked") {
		body = br
	} else if cl := out.header.get("Content-Length"); cl != "" {
		n, err := strconv.Atoi(cl)
		if err != nil {
			out.note = "bad Content-Length " + strconv.Quote(cl)
			return out
		}
		body = io.LimitReader(br, int64(n))
	}

	const cap = 1 << 20
	var buf bytes.Buffer
	limited := &limitedReader{r: body, max: cap}
	if _, err := io.Copy(&buf, limited); err != nil {
		out.body = buf.Bytes()
		out.note = "body read failed: " + err.Error()
		return out
	}
	out.bodyTrunc = limited.truncated
	out.body = buf.Bytes()

	// Content-Length framing must be satisfied exactly, or the response is
	// incomplete even if the connection stayed open.
	if cl := out.header.get("Content-Length"); cl != "" && !limited.truncated {
		n, _ := strconv.Atoi(cl)
		if n != buf.Len() {
			out.note = fmt.Sprintf("short body: Content-Length=%d read=%d", n, buf.Len())
			return out
		}
	}
	out.complete = true
	return out
}

type limitedReader struct {
	r         io.Reader
	max       int
	read      int
	truncated bool
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.read >= l.max {
		// Probe one more byte to learn whether the body was longer than the cap.
		var one [1]byte
		n, err := l.r.Read(one[:])
		if n > 0 {
			l.truncated = true
			return 0, nil
		}
		return 0, err
	}
	if len(p) > l.max-l.read {
		p = p[:l.max-l.read]
	}
	n, err := l.r.Read(p)
	l.read += n
	return n, err
}

// dial opens one connection to the service.
func dial() (net.Conn, error) {
	c, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// buildRequest renders a request. hdrs are "Name: value"; body may be nil.
func buildRequest(method, path string, hdrs []string, body []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\n", method, path)
	b.WriteString("Host: 127.0.0.1:8080\r\n")
	for _, h := range hdrs {
		b.WriteString(h + "\r\n")
	}
	if body != nil {
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	}
	b.WriteString("Connection: close\r\n\r\n")
	if body != nil {
		b.Write(body)
	}
	return b.Bytes()
}

// one performs a single request and returns the response.
func one(method, path string, hdrs []string, body []byte) resp {
	c, err := dial()
	if err != nil {
		return resp{note: "dial: " + err.Error()}
	}
	defer c.Close()
	start := time.Now()
	_ = c.SetWriteDeadline(time.Now().Add(deadline))
	if _, err := c.Write(buildRequest(method, path, hdrs, body)); err != nil {
		return resp{note: "write: " + err.Error()}
	}
	r := readResp(c, deadline)
	r.duration = time.Since(start)
	return r
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: advtool <subcommand> ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "ready":
		err = cmdReady(os.Args[2:])
	case "req":
		err = cmdReq(os.Args[2:])
	case "raw":
		err = cmdRaw(os.Args[2:])
	case "burst":
		err = cmdBurst(os.Args[2:])
	case "bigbody":
		err = cmdBigBody(os.Args[2:])
	case "bigheader":
		err = cmdBigHeader(os.Args[2:])
	case "bigline":
		err = cmdBigLine(os.Args[2:])
	case "abort":
		err = cmdAbort(os.Args[2:])
	case "load":
		err = cmdLoad(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad integer:", s)
		os.Exit(2)
	}
	return n
}

// cmdReady polls /health and reports the time to first 200 (C-RUN-2).
func cmdReady(args []string) error {
	budget := 10
	if len(args) > 0 {
		budget = mustAtoi(args[0])
	}
	deadline2 := time.Now().Add(time.Duration(budget) * time.Second)
	start := time.Now()
	for time.Now().Before(deadline2) {
		r := one("GET", "/health", nil, nil)
		if r.complete && r.status == 200 {
			fmt.Printf("READY ms=%d status=%d body=%s\n",
				time.Since(start).Milliseconds(), r.status, strings.TrimSpace(string(r.body)))
			if time.Since(start) > 10*time.Second {
				return fmt.Errorf("C-RUN-2 breach: ready only after 10s")
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("NOT_READY budget_s=%d\n", budget)
	return fmt.Errorf("no 200 /health within %ds", budget)
}

func printResp(r resp) {
	fmt.Printf("STATUS=%d\n", r.status)
	fmt.Printf("PROTO=%s\n", r.proto)
	for _, k := range r.header.sortedKeys() {
		fmt.Printf("HDR %s: %s\n", k, r.header[k])
	}
	fmt.Printf("BODY=%s\n", string(r.body))
	fmt.Printf("COMPLETE=%t\n", r.complete)
	if r.note != "" {
		fmt.Printf("NOTE=%s\n", r.note)
	}
}

// cmdReq performs one request: advtool req <method> <path> [Name: value ...]
// with the literal body given by BODY=<bytes> in the environment-free form
// --body <string>; --body-N <n> generates n filler bytes instead.
func cmdReq(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: req <method> <path> [-H 'Name: value']... [--body s | --body-N n]")
	}
	method, path := args[0], args[1]
	var hdrs []string
	var body []byte
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		switch {
		case rest[i] == "-H" && i+1 < len(rest):
			i++
			hdrs = append(hdrs, rest[i])
		case rest[i] == "--body" && i+1 < len(rest):
			i++
			body = []byte(rest[i])
		case rest[i] == "--body-N" && i+1 < len(rest):
			i++
			body = bytes.Repeat([]byte("A"), mustAtoi(rest[i]))
		default:
			return fmt.Errorf("unexpected argument %q", rest[i])
		}
	}
	printResp(one(method, path, hdrs, body))
	return nil
}

// cmdRaw sends arbitrary bytes and reports what came back.
func cmdRaw(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: raw <hex-bytes>")
	}
	raw, err := hexDecode(args[0])
	if err != nil {
		return err
	}
	c, err := dial()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(deadline))
	if _, err := c.Write(raw); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	printResp(readResp(c, deadline))
	return nil
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("hex string must have even length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		n, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("bad hex at %d: %w", i, err)
		}
		out[i] = byte(n)
	}
	return out, nil
}

// cmdBurst opens n connections, then releases them simultaneously, so the
// requests genuinely contend (C-RES-6, C-INV-1 style simultaneity).
func cmdBurst(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: burst <n> <method> <path> [-H k: v] [--body s]")
	}
	n := mustAtoi(args[0])
	method, path := args[1], args[2]
	var hdrs []string
	var body []byte
	rest := args[3:]
	for i := 0; i < len(rest); i++ {
		switch {
		case rest[i] == "-H" && i+1 < len(rest):
			i++
			hdrs = append(hdrs, rest[i])
		case rest[i] == "--body" && i+1 < len(rest):
			i++
			body = []byte(rest[i])
		default:
			return fmt.Errorf("unexpected argument %q", rest[i])
		}
	}

	conns := make([]net.Conn, n)
	for i := 0; i < n; i++ {
		c, err := dial()
		if err != nil {
			return fmt.Errorf("dial %d/%d: %w", i, n, err)
		}
		conns[i] = c
		defer c.Close()
	}
	req := buildRequest(method, path, hdrs, body)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make([]resp, n)
		gate    = make(chan struct{})
	)
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int, c net.Conn) {
			defer wg.Done()
			<-gate // barrier: all sockets are open before any byte is sent
			_ = c.SetWriteDeadline(time.Now().Add(deadline))
			if _, err := c.Write(req); err != nil {
				mu.Lock()
				results[i] = resp{note: "write: " + err.Error()}
				mu.Unlock()
				return
			}
			r := readResp(c, deadline)
			mu.Lock()
			results[i] = r
			mu.Unlock()
		}(i, conns[i])
	}
	close(gate)
	wg.Wait()
	elapsed := time.Since(start)

	hist := map[int]int{}
	var incomplete, fiveXX int
	for _, r := range results {
		if !r.complete {
			incomplete++
			continue
		}
		hist[r.status]++
		if r.status >= 500 {
			fiveXX++
		}
	}
	var keys []int
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d=%d", k, hist[k]))
	}
	fmt.Printf("BURST n=%d method=%s path=%s elapsed_ms=%d\n", n, method, path, elapsed.Milliseconds())
	fmt.Printf("STATUS_HIST %s\n", strings.Join(parts, " "))
	fmt.Printf("INCOMPLETE=%d\n", incomplete)
	fmt.Printf("FIVE_XX=%d\n", fiveXX)
	fmt.Printf("MAX_STATUS=%d\n", maxStatus(results))

	if fiveXX > 0 {
		return fmt.Errorf("C-ERR-500 breach: %d responses were 5xx", fiveXX)
	}
	if incomplete > 0 {
		return fmt.Errorf("C-ERR-500 breach: %d/%d responses incomplete (dropped connection)", incomplete, n)
	}
	return nil
}

func maxStatus(rs []resp) int {
	m := 0
	for _, r := range rs {
		if r.complete && r.status > m {
			m = r.status
		}
	}
	return m
}

// cmdBigBody sends a request whose body is n filler bytes. An oversized body
// must still get a complete response (C-ERR-500), whether the route reads it
// or not.
func cmdBigBody(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: bigbody <n-bytes> <method> <path>")
	}
	n := mustAtoi(args[0])
	method, path := args[1], args[2]
	// 1 MiB filler chunks keep memory flat regardless of n.
	const chunk = 1 << 20
	body := bytes.Repeat([]byte("A"), chunk)
	c, err := dial()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	head := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", method, path, n)
	if _, err := io.WriteString(c, head); err != nil {
		return fmt.Errorf("write head: %w", err)
	}
	sent := 0
	var werr error
	for sent < n {
		k := n - sent
		if k > chunk {
			k = chunk
		}
		m, err := c.Write(body[:k])
		sent += m
		if err != nil {
			werr = err
			break
		}
	}
	r := readResp(c, 60*time.Second)
	fmt.Printf("BIGBODY requested=%d sent=%d method=%s path=%s\n", n, sent, method, path)
	if werr != nil {
		fmt.Printf("WRITE_ERR=%v\n", werr)
	}
	printResp(r)
	if !r.complete {
		return fmt.Errorf("C-ERR-500 breach: oversized body of %d bytes got no complete response", n)
	}
	return nil
}

// cmdBigHeader sends a request whose header block is n bytes, generated here so
// the payload never has to fit in an argv. Go's server caps headers at
// MaxHeaderBytes (1 MiB here), so a larger block must still produce a complete
// response rather than a dropped connection (C-ERR-500).
func cmdBigHeader(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bigheader <n-bytes> [path]")
	}
	n := mustAtoi(args[0])
	path := "/health"
	if len(args) > 1 {
		path = args[1]
	}
	const chunk = 64 << 10
	pad := bytes.Repeat([]byte("A"), chunk)

	c, err := dial()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	head := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nConnection: close\r\nX-Pad: ", path)
	if _, err := io.WriteString(c, head); err != nil {
		return fmt.Errorf("write head: %w", err)
	}
	sent := len(head)
	// One pad value, then CRLF CRLF: this is what busybox nc and curl send, so
	// the result is comparable with an independent client.
	padLen := n - sent - 4
	if padLen < 0 {
		padLen = 0
	}
	var werr error
	for padLen > 0 {
		k := padLen
		if k > chunk {
			k = chunk
		}
		m, err := c.Write(pad[:k])
		sent += m
		padLen -= m
		if err != nil {
			werr = err
			break
		}
	}
	if werr == nil {
		if _, err := io.WriteString(c, "\r\n\r\n"); err != nil {
			werr = err
		} else {
			sent += 4
		}
	}
	r := readResp(c, 60*time.Second)
	fmt.Printf("BIGHEADER requested=%d sent=%d path=%s\n", n, sent, path)
	if werr != nil {
		fmt.Printf("WRITE_ERR=%v\n", werr)
	}
	printResp(r)
	if r.complete && r.status >= 500 {
		return fmt.Errorf("C-ERR-500 breach: oversized header block got %d", r.status)
	}
	if !r.complete {
		return fmt.Errorf("C-ERR-500 breach: oversized header block got no complete response: %s", r.note)
	}
	return nil
}

// cmdBigLine sends a request whose request line is about n bytes, by padding the
// request target. The request line is not covered by MaxHeaderBytes, so a very
// long target exercises a different parser limit than a long header value; a
// complete, non-5xx response is still required (C-ERR-500).
func cmdBigLine(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bigline <n-bytes> [method]")
	}
	n := mustAtoi(args[0])
	method := "GET"
	if len(args) > 1 {
		method = args[1]
	}
	const chunk = 64 << 10
	pad := bytes.Repeat([]byte("A"), chunk)

	c, err := dial()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	head := fmt.Sprintf("%s /", method)
	if _, err := io.WriteString(c, head); err != nil {
		return fmt.Errorf("write head: %w", err)
	}
	sent := len(head)
	padLen := n - sent - len(" HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nConnection: close\r\n\r\n")
	if padLen < 0 {
		padLen = 0
	}
	var werr error
	for padLen > 0 {
		k := padLen
		if k > chunk {
			k = chunk
		}
		m, err := c.Write(pad[:k])
		sent += m
		padLen -= m
		if err != nil {
			werr = err
			break
		}
	}
	if werr == nil {
		if _, err := io.WriteString(c, " HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nConnection: close\r\n\r\n"); err != nil {
			werr = err
		} else {
			sent += len(" HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nConnection: close\r\n\r\n")
		}
	}
	r := readResp(c, 60*time.Second)
	fmt.Printf("BIGLINE requested=%d sent=%d method=%s\n", n, sent, method)
	if werr != nil {
		fmt.Printf("WRITE_ERR=%v\n", werr)
	}
	printResp(r)
	if r.complete && r.status >= 500 {
		return fmt.Errorf("C-ERR-500 breach: %d-byte request line got %d", n, r.status)
	}
	if !r.complete {
		return fmt.Errorf("C-ERR-500 breach: %d-byte request line got no complete response: %s", n, r.note)
	}
	return nil
}

// cmdAbort sends a request that declares a body, delivers only part of it and
// then closes the write side. The client stops sending mid-request, so the server
// sees a truncated body; it must still answer with a complete response rather
// than dropping the connection (C-ERR-500).
func cmdAbort(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: abort <method> <path> <declared-body-bytes>")
	}
	method, path, declared := args[0], args[1], mustAtoi(args[2])
	const chunk = 64 << 10
	pad := bytes.Repeat([]byte("A"), chunk)

	c, err := dial()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	head := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: 127.0.0.1:8080\r\nContent-Length: %d\r\n\r\n",
		method, path, declared)
	if _, err := io.WriteString(c, head); err != nil {
		return fmt.Errorf("write head: %w", err)
	}
	// Deliver half the declared body, then give up the write side.
	partial := declared / 2
	var werr error
	for partial > 0 {
		k := partial
		if k > chunk {
			k = chunk
		}
		m, err := c.Write(pad[:k])
		partial -= m
		if err != nil {
			werr = err
			break
		}
	}
	if werr == nil {
		// Close the write side so the server sees EOF mid-body; the read side
		// stays open to collect whatever it answers.
		if cw, ok := c.(interface{ CloseWrite() error }); ok {
			werr = cw.CloseWrite()
		}
	}
	r := readResp(c, 30*time.Second)
	fmt.Printf("ABORT method=%s path=%s declared=%d\n", method, path, declared)
	if werr != nil {
		fmt.Printf("WRITE_ERR=%v\n", werr)
	}
	printResp(r)
	if r.complete && r.status >= 500 {
		return fmt.Errorf("C-ERR-500 breach: truncated %s %s got %d", method, path, r.status)
	}
	if !r.complete {
		return fmt.Errorf("C-ERR-500 breach: truncated %s %s got no complete response: %s", method, path, r.note)
	}
	return nil
}

// cmdLoad drives sustained mixed traffic and asserts the invariants that must
// hold under load: never 5xx, always a complete response, JSON content type.
func cmdLoad(args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("usage: load <seconds> <concurrency> <path> <method>")
	}
	secs := mustAtoi(args[0])
	conc := mustAtoi(args[1])
	path, method := args[2], args[3]

	var (
		mu       sync.Mutex
		total    int
		fiveXX   int
		incompl  int
		badCT    int
		badJSON  int
		statuses = map[int]int{}
		firstErr = map[string]int{}
	)
	stop := time.Now().Add(time.Duration(secs) * time.Second)
	var wg sync.WaitGroup
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				r := one(method, path, nil, nil)
				mu.Lock()
				total++
				if !r.complete {
					incompl++
					firstErr[r.note]++
				} else {
					statuses[r.status]++
					if r.status >= 500 {
						fiveXX++
					}
					if !strings.HasPrefix(strings.ToLower(r.contentType()), "application/json") {
						badCT++
					} else if !r.isSingleJSONObject() {
						badJSON++
					}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	var keys []int
	for k := range statuses {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d=%d", k, statuses[k]))
	}
	fmt.Printf("LOAD secs=%d concurrency=%d path=%s method=%s total=%d\n", secs, conc, path, method, total)
	fmt.Printf("STATUS_HIST %s\n", strings.Join(parts, " "))
	fmt.Printf("FIVE_XX=%d\n", fiveXX)
	fmt.Printf("INCOMPLETE=%d\n", incompl)
	fmt.Printf("BAD_CONTENT_TYPE=%d\n", badCT)
	fmt.Printf("BAD_JSON_SHAPE=%d\n", badJSON)
	if len(firstErr) > 0 {
		fmt.Printf("FIRST_ERRORS %s\n", formatCounts(firstErr))
	}
	if fiveXX > 0 || incompl > 0 || badCT > 0 || badJSON > 0 {
		return fmt.Errorf("C-ERR-500/C-REP-HDR breach: fiveXX=%d incomplete=%d badCT=%d badJSON=%d",
			fiveXX, incompl, badCT, badJSON)
	}
	return nil
}

func formatCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func (h httpHeader) sortedKeys() []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
