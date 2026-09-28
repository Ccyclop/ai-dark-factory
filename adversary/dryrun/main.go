// Command stub is NOT the practice service and is NOT evidence of anything.
//
// It exists for one reason: adversary/attack-state.sh must be provably working
// before the real routes exist, otherwise its first real run at a gate is also
// its first ever run and any quoting or arithmetic bug in it is mistaken for a
// contract break. This stub implements just enough of CONTRACT.md section 4 to
// let that script exercise every branch, and it is built only when the script
// is run with --stub.
//
// It is deliberately naive in the ways a real implementation could be wrong
// (one mutex, no database, no timeouts) and deliberately correct about status
// codes and representations, because that is what the campaign script reads.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const maxBody = 1048576

type item struct {
	ID        int64
	Name      string
	Stock     int64
	Available int64
}

type reservation struct {
	ID       int64
	ItemID   int64
	Quantity int64
	Status   string
}

type store struct {
	mu      sync.Mutex
	items   map[int64]*item
	res     map[int64]*reservation
	nextI   int64
	nextR   int64
	idem    map[string]stored
	idemSeq int64
}

type stored struct {
	status int
	body   string
	fp     string
}

func main() {
	s := &store{items: map[int64]*item{}, res: map[int64]*reservation{}, idem: map[string]stored{}, nextI: 1, nextR: 1}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.route)
	srv := &http.Server{Addr: ":8080", Handler: mux, DisableGeneralOptionsHandler: true}
	log.Printf("stub listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}

func (s *store) route(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/health":
		if r.Method != "GET" && r.Method != "HEAD" {
			notAllowed(w, "GET", "HEAD")
			return
		}
		s.health(w, r)
	case p == "/items":
		if r.Method != "POST" {
			notAllowed(w, "POST")
			return
		}
		s.createItem(w, r)
	case isSegmentPath(p, "/items/"):
		if r.Method != "GET" && r.Method != "HEAD" {
			notAllowed(w, "GET", "HEAD")
			return
		}
		s.getItem(w, r)
	case p == "/reservations":
		if r.Method != "POST" {
			notAllowed(w, "POST")
			return
		}
		s.reserve(w, r)
	case isSegmentPath(p, "/reservations/"):
		if r.Method != "DELETE" {
			notAllowed(w, "DELETE")
			return
		}
		s.cancelRes(w, r)
	default:
		writeErr(w, 404, "not found")
	}
}

// isSegmentPath reports whether p is prefix + exactly one non-empty segment.
func isSegmentPath(p, prefix string) bool {
	return strings.HasPrefix(p, prefix) && len(p) > len(prefix) && !strings.Contains(p[len(prefix):], "/")
}

func notAllowed(w http.ResponseWriter, allowed ...string) {
	sort.Strings(allowed)
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeErr(w, 405, "method not allowed")
}

func (s *store) health(w http.ResponseWriter, _ *http.Request) {
	writeRaw(w, 200, []byte(`{"status": "ok"}`))
}

// --- idempotency -------------------------------------------------------------

// keyCheck validates the header (C-IDEM-5) and returns the key, or writes the
// error response and reports that the request must not proceed.
func keyCheck(w http.ResponseWriter, r *http.Request) (string, bool) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) > 1 {
		writeErr(w, 400, "too many Idempotency-Key headers")
		return "", false
	}
	if len(keys) == 0 {
		return "", true
	}
	k := keys[0]
	if len(k) < 1 || len(k) > 255 {
		writeErr(w, 400, "Idempotency-Key must be 1..255 bytes")
		return "", false
	}
	return k, true
}

func fingerprint(r *http.Request, body []byte) string {
	return r.Method + " " + r.URL.Path + " " + string(body)
}

// idempotent wraps an operation: replay returns the stored response verbatim
// (C-IDEM-2), a different method/path/body returns 409 (C-IDEM-3).
func (s *store) idempotent(w http.ResponseWriter, r *http.Request, key, fp string, op func() (int, string)) {
	if key == "" {
		st, body := op()
		writeRaw(w, st, []byte(body))
		return
	}
	if prev, ok := s.idem[key]; ok {
		if prev.fp != fp {
			writeErr(w, 409, "idempotency key reused with a different request")
			return
		}
		writeRaw(w, prev.status, []byte(prev.body))
		return
	}
	st, body := op()
	s.idem[key] = stored{status: st, body: body, fp: fp}
	writeRaw(w, st, []byte(body))
}

// --- body decoding -----------------------------------------------------------

type fields struct {
	num  map[string]int64
	str  map[string]string
	ok   map[string]bool
	json map[string]json.RawMessage
	raw  []byte
}

// decodeBody enforces the C-ERR-400 list: one JSON object, no trailing content,
// required fields present, non-null, of the right type, integers without a
// fraction or exponent, and at most maxBody bytes.
func decodeBody(r *http.Request, want ...string) (*fields, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return nil, false
	}
	if len(raw) > maxBody {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var m map[string]json.RawMessage
	if err := dec.Decode(&m); err != nil {
		return nil, false
	}
	if m == nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false // trailing content
	}
	f := &fields{num: map[string]int64{}, str: map[string]string{}, ok: map[string]bool{}, json: m, raw: raw}
	for _, k := range want {
		v, present := m[k]
		if !present || string(v) == "null" {
			return nil, false
		}
		f.ok[k] = true
	}
	return f, true
}

func (f *fields) intField(w http.ResponseWriter, k string, min, max int64) (int64, bool) {
	raw := string(f.json[k])
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || strings.ContainsAny(raw, ".eE") {
		writeErr(w, 400, "field "+k+" must be an integer")
		return 0, false
	}
	if n < min || n > max {
		writeErr(w, 400, "field "+k+" out of range")
		return 0, false
	}
	return n, true
}

func (f *fields) strField(w http.ResponseWriter, k string, minRunes, maxRunes int) (string, bool) {
	var s string
	if err := json.Unmarshal(f.json[k], &s); err != nil {
		writeErr(w, 400, "field "+k+" must be a string")
		return "", false
	}
	if strings.TrimSpace(s) == "" || len([]rune(s)) < minRunes || len([]rune(s)) > maxRunes {
		writeErr(w, 400, "field "+k+" out of range")
		return "", false
	}
	return s, true
}

// --- operations --------------------------------------------------------------

func itemJSON(i *item) string {
	return fmt.Sprintf(`{"id":%d,"name":%s,"stock":%d,"available":%d}`,
		i.ID, strconv.Quote(i.Name), i.Stock, i.Available)
}

func resJSON(r *reservation) string {
	return fmt.Sprintf(`{"id":%d,"item_id":%d,"quantity":%d,"status":%s}`,
		r.ID, r.ItemID, r.Quantity, strconv.Quote(r.Status))
}

func (s *store) createItem(w http.ResponseWriter, r *http.Request) {
	key, ok := keyCheck(w, r)
	if !ok {
		return
	}
	f, ok := decodeBody(r, "name", "stock")
	if !ok {
		writeErr(w, 400, "invalid body")
		return
	}
	name, ok := f.strField(w, "name", 1, 200)
	if !ok {
		return
	}
	stock, ok := f.intField(w, "stock", 0, 1000000000)
	if !ok {
		return
	}
	fp := fingerprint(r, f.raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idempotent(w, r, key, fp, func() (int, string) {
		it := &item{ID: s.nextI, Name: name, Stock: stock, Available: stock}
		s.nextI++
		s.items[it.ID] = it
		return 201, itemJSON(it)
	})
}

func (s *store) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, found := s.items[id]
	if !found {
		writeErr(w, 404, "no such item")
		return
	}
	writeRaw(w, 200, []byte(itemJSON(it)))
}

func (s *store) reserve(w http.ResponseWriter, r *http.Request) {
	key, ok := keyCheck(w, r)
	if !ok {
		return
	}
	f, ok := decodeBody(r, "item_id", "quantity")
	if !ok {
		writeErr(w, 400, "invalid body")
		return
	}
	itemID, ok := f.intField(w, "item_id", -9223372036854775808, 9223372036854775807)
	if !ok {
		return
	}
	qty, ok := f.intField(w, "quantity", 1, 1000000000)
	if !ok {
		return
	}
	fp := fingerprint(r, f.raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idempotent(w, r, key, fp, func() (int, string) {
		it, found := s.items[itemID]
		if !found {
			return 404, `{"error":"no such item"}`
		}
		if it.Available < qty {
			return 409, `{"error":"insufficient stock"}`
		}
		it.Available -= qty
		rs := &reservation{ID: s.nextR, ItemID: itemID, Quantity: qty, Status: "active"}
		s.nextR++
		s.res[rs.ID] = rs
		return 201, resJSON(rs)
	})
}

func (s *store) cancelRes(w http.ResponseWriter, r *http.Request) {
	key, ok := keyCheck(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	fp := fingerprint(r, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idempotent(w, r, key, fp, func() (int, string) {
		rs, found := s.res[id]
		if !found {
			return 404, `{"error":"no such reservation"}`
		}
		if rs.Status == "active" {
			rs.Status = "cancelled"
			s.items[rs.ItemID].Available += rs.Quantity
		}
		return 200, resJSON(rs)
	})
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Path
	raw = raw[strings.LastIndex(raw, "/")+1:]
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != raw {
		writeErr(w, 404, "no such id")
		return 0, false
	}
	return n, true
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	b, _ := json.Marshal(map[string]string{"error": msg})
	writeRaw(w, status, b)
}

func writeRaw(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}
