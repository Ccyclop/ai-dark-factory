package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// C-ERR-500 and S-11's shape: requests that arrive at once must all be answered
// completely, with no 5xx among them. The service runs with --cpus=1, so these
// bursts are deliberately small enough to stay quick but large enough to
// overlap: the barrier below releases every goroutine at the same instant.

// C-OP-HEALTH: 50 identical health requests at once, all 50 answered 200 with
// the same body.
func TestC_OP_HEALTH_50_simultaneous_requests(t *testing.T) {
	const n = 50
	results := make([]result, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	c := newClient()
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = doWith(c, http.MethodGet, "/health", nil, nil)
		}()
	}
	close(start)
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		mustStatus(t, results[i], http.StatusOK)
		mustHealthBody(t, results[i])
		if string(results[i].Body) != string(results[0].Body) {
			t.Fatalf("request %d: body %q, want %q", i, results[i].Body, results[0].Body)
		}
	}
}

// C-ERR-500: a mixed burst on one connection pool — the health route, the wrong
// method, unknown paths, large bodies and malformed requests all at once. Every
// one of them must come back complete and without a 5xx.
func TestC_ERR_500_mixed_simultaneous_requests(t *testing.T) {
	const n = 120
	type job struct {
		kind   string
		method string
		target string
		body   []byte
		raw    *rawReq
	}
	var jobs []job
	for i := range n {
		switch i % 6 {
		case 0:
			jobs = append(jobs, job{kind: "health", method: http.MethodGet, target: "/health"})
		case 1:
			jobs = append(jobs, job{kind: "wrong-method", method: http.MethodPost, target: "/health",
				body: []byte(`{"name":"x","stock":1}`)})
		case 2:
			jobs = append(jobs, job{kind: "unknown-path", method: http.MethodGet, target: "/nope"})
		case 3:
			jobs = append(jobs, job{kind: "long-path", method: http.MethodGet,
				target: "/" + strings.Repeat("z", 6000)})
		case 4:
			jobs = append(jobs, job{kind: "large-body", method: http.MethodPost, target: "/health",
				body: []byte(strings.Repeat("b", 256*1024))})
		case 5:
			jobs = append(jobs, job{kind: "malformed", raw: &rawReq{
				Method: http.MethodGet, Target: "/health", Headers: []string{"Content-Length: 99"},
				Body: []byte("short")}})
		}
	}
	c := newClient()
	var wg sync.WaitGroup
	start := make(chan struct{})
	var bad atomic.Int64
	for i := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			j := jobs[i]
			if j.raw != nil {
				r, err := exchange(t, *j.raw)
				if err != nil {
					var none *noResponseError
					if !errors.As(err, &none) {
						bad.Add(1)
						t.Errorf("%s: %v", j.kind, err)
					}
					return
				}
				if r.Status >= 500 {
					bad.Add(1)
					t.Errorf("%s: got %d, want no 5xx", j.kind, r.Status)
				}
				return
			}
			r, err := doWith(c, j.method, j.target, j.body, nil)
			if err != nil {
				bad.Add(1)
				t.Errorf("%s: %v", j.kind, err)
				return
			}
			if r.Status >= 500 {
				bad.Add(1)
				t.Errorf("%s: got %d, want no 5xx", j.kind, r.Status)
			}
		}()
	}
	close(start)
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("%d of %d concurrent requests misbehaved", bad.Load(), len(jobs))
	}
	assertStillHealthy(t)
}

// C-ERR-500: many connections at once, opened and closed without a request, then
// a normal request that must still be answered.
func TestC_ERR_500_many_simultaneous_connections(t *testing.T) {
	const n = 100
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c, err := dialRaw()
			if err != nil {
				t.Errorf("dial: %v", err)
				return
			}
			c.Close()
		}()
	}
	close(start)
	wg.Wait()
	mustHealth(t, do(t, http.MethodGet, "/health", nil, nil))
}

// C-ERR-500: the service keeps answering after the whole suite has abused it, and
// the answers stay identical.
func TestC_ERR_500_healthy_after_the_abuse(t *testing.T) {
	var first []byte
	for i := range 10 {
		r := do(t, http.MethodGet, "/health", nil, nil)
		mustHealth(t, r)
		if i == 0 {
			first = r.Body
			continue
		}
		if string(r.Body) != string(first) {
			t.Fatalf("after abuse %d: body %q, want %q", i, r.Body, first)
		}
	}
	t.Logf("health after the abuse: %s", strconv.Quote(string(first)))
}
