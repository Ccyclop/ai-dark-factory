package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
)

// maxDrainBytes bounds how much of an unread request body is consumed after a
// response is written. Reading the rest lets the client finish sending, so a
// large body gets its full response instead of a connection reset (C-ERR-500).
const maxDrainBytes = 64 << 20

type server struct {
	db *sql.DB
}

// methodSet maps an HTTP method to its handler for one route.
type methodSet map[string]http.HandlerFunc

func newServer(db *sql.DB) http.Handler {
	return &server{db: db}
}

// route returns the handlers for path, or nil if path is not a route.
// Routes are exact paths (CONTRACT section 4); no redirects or cleaning.
func (s *server) route(path string) methodSet {
	switch path {
	case "/health":
		return methodSet{http.MethodGet: s.handleHealth}
	}
	return nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := r.Body
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("panic serving %s %q: %v", r.Method, r.URL.Path, rec)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		if body != nil {
			io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes))
		}
	}()

	methods := s.route(r.URL.Path)
	if methods == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	h, ok := methods[r.Method]
	if !ok && r.Method == http.MethodHead {
		h, ok = methods[http.MethodGet]
	}
	if !ok {
		w.Header().Set("Allow", allowHeader(methods))
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h(w, r)
}

func allowHeader(methods methodSet) string {
	names := make([]string, 0, len(methods)+1)
	for m := range methods {
		names = append(names, m)
	}
	if _, ok := methods[http.MethodGet]; ok {
		if _, ok := methods[http.MethodHead]; !ok {
			names = append(names, http.MethodHead)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeRaw(w, http.StatusOK, []byte(`{"status": "ok"}`))
}

// writeJSON writes v as the JSON response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("encode response: %v", err)
		status, b = http.StatusInternalServerError, []byte(`{"error":"internal error"}`)
	}
	writeRaw(w, status, b)
}

// writeError writes the contracted error body {"error": msg} (C-REP-ERR).
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeRaw(w http.ResponseWriter, status int, b []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	w.Write(b)
}
