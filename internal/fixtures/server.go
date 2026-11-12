package fixtures

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Sheet IDs used by demo mode.
const (
	InboundSheetID  = "demo-inbound-sheet"
	OutboundSheetID = "demo-outbound-sheet"
	DemoAccessToken = "demo-access-token"
	DemoRefresh     = "demo-refresh-token"
	DemoClientID    = "demo-client.apps.nventures-demo.test"
)

// InboundHeaders is the six-column inbound sheet header row.
var InboundHeaders = []string{"Company Name", "Description", "HQ", "Contact", "Status", "Notes"}

// OutboundHeaders imitates the legacy 38-column sheet, including the
// historical typo headers, duplicate Extra Notes and unknown columns.
var OutboundHeaders = []string{"Company Name", "Country", "Sector", "Business Model", "Stage", "Founded year", "Headquarters", "Website", "Company LinkedIn", "Company Description", "Product Descriptoon", "Target Market", "Team Size", "Number of Customers", "Notable Customers", "Revenue Generated", "Unique Value Propsotions", "Competitors", "Vision", "Founder Name", "Founder LinkedIn", "Last Round", "Notable Investors", "Total Funding USD", "Outreach Status", "Status", "IC Recommendation", "Reason", "Extra Notes", "Updates", "Extra Notes", "Analyst", "Date Contacted", "Next Step", "Meeting Date", "Deal Lead", "Priority", "Internal Score"}

// Fault forces a single failure on the next Sheets append.
type Fault string

const (
	FaultNone          Fault = ""
	FaultBeforeWrite   Fault = "500_before_write" // nothing written, 500 returned
	FaultLostResponse  Fault = "lost_response"    // row written, connection dropped
	FaultNoRange       Fault = "no_range"         // row written, response lacks updatedRange
	FaultBadReadback   Fault = "bad_readback"     // read-back returns altered content once
	FaultRateLimitRead Fault = "rate_limit_read"  // next read returns 429 with Retry-After
)

// Server is the deterministic local fixture server.
type Server struct {
	mu        sync.Mutex
	Sheets    map[string]map[string][][]string `json:"sheets"`
	path      string
	fault     Fault
	readFault bool
	Appends   atomic.Int64
	Reads     atomic.Int64
	Requests  atomic.Int64
	rateHits  map[string]int
	codes     map[string]string // auth code -> challenge
	ln        net.Listener
	srv       *http.Server
	URL       string
	Addr      string
	Mail      []Mail
	PageSize  int
}

// NewServer creates a fixture server. If statePath is non-empty, sheet
// contents persist there so repeated rehearsals detect existing rows.
func NewServer(statePath string) *Server {
	s := &Server{path: statePath, rateHits: map[string]int{}, codes: map[string]string{}, Mail: Mailbox(), PageSize: 5}
	s.Sheets = map[string]map[string][][]string{}
	if statePath != "" {
		if b, err := os.ReadFile(statePath); err == nil {
			_ = json.Unmarshal(b, &s.Sheets)
		}
	}
	if s.Sheets[InboundSheetID] == nil {
		s.Sheets[InboundSheetID] = map[string][][]string{"Pitches": {append([]string{}, InboundHeaders...)}}
	}
	if s.Sheets[OutboundSheetID] == nil {
		control := [][]string{{"Partner Name", "Last Sourced UTC", "Run Count", "Last Run Added"}}
		partners := [][]string{{"Company Name", "Website", "nVentures Relevance"}}
		for _, p := range Partners() {
			partners = append(partners, []string{p.Name, p.Website, "Yes"})
		}
		partners = append(partners, []string{"Irrelevant Fund (demo)", "", "No"})
		s.Sheets[OutboundSheetID] = map[string][][]string{"Active Sourcing": {append([]string{}, OutboundHeaders...)}, "Partner List": partners, "Sourcing Control": control}
	}
	return s
}

// Start listens on an ephemeral loopback port.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.ln = ln
	s.Addr = ln.Addr().String()
	s.URL = "http://" + s.Addr
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second}
	go s.srv.Serve(ln)
	return nil
}

// Close stops the server.
func (s *Server) Close() {
	if s.srv != nil {
		s.srv.Close()
	}
}

// SetFault arms a one-shot Sheets append fault.
func (s *Server) SetFault(f Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f == FaultRateLimitRead {
		s.readFault = true
		return
	}
	s.fault = f
}

// Rows returns a copy of a tab.
func (s *Server) Rows(sheet, tab string) [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][]string
	for _, r := range s.Sheets[sheet][tab] {
		out = append(out, append([]string{}, r...))
	}
	return out
}

// SetRows replaces a tab (tests only).
func (s *Server) SetRows(sheet, tab string, rows [][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sheets[sheet][tab] = rows
	s.saveLocked()
}

func (s *Server) saveLocked() {
	if s.path == "" {
		return
	}
	b, _ := json.MarshalIndent(s.Sheets, "", " ")
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Requests.Add(1)
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.HasSuffix(host, DemoSuffix) {
		s.site(w, r, host)
		return
	}
	switch {
	case r.URL.Path == "/o/oauth2/auth":
		s.authorize(w, r)
		return
	case r.URL.Path == "/token":
		s.token(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+DemoAccessToken {
		writeJSON(w, 401, map[string]any{"error": map[string]any{"code": 401, "message": "Request had invalid authentication credentials."}})
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/"):
		s.gmail(w, r)
	case strings.HasPrefix(r.URL.Path, "/v4/spreadsheets/"):
		s.sheets(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) site(w http.ResponseWriter, r *http.Request, host string) {
	slug := strings.TrimSuffix(host, DemoSuffix)
	if slug == "indusventures" && r.URL.Path == "/portfolio" {
		s.mu.Lock()
		s.rateHits[host]++
		n := s.rateHits[host]
		s.mu.Unlock()
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
	}
	body, ct, code, ok := sitePage(host, r.URL.Path, r.URL.RawQuery)
	if !ok {
		http.NotFound(w, r)
		return
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(body)))
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Type", ct+"; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("client_id") != DemoClientID {
		http.Error(w, "invalid request", 400)
		return
	}
	code := fmt.Sprintf("demo-code-%d", time.Now().UnixNano())
	s.mu.Lock()
	s.codes[code] = q.Get("code_challenge")
	s.mu.Unlock()
	v := redirect.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	redirect.RawQuery = v.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.PostForm.Get("client_id") != DemoClientID {
		writeJSON(w, 401, map[string]string{"error": "invalid_client"})
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "refresh_token":
		if r.PostForm.Get("refresh_token") != DemoRefresh {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
	case "authorization_code":
		s.mu.Lock()
		challenge, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code"))
		s.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": DemoAccessToken, "refresh_token": DemoRefresh, "expires_in": 3600, "token_type": "Bearer", "scope": "https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/spreadsheets"})
		return
	case "urn:ietf:params:oauth:grant-type:jwt-bearer":
	default:
		writeJSON(w, 400, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": DemoAccessToken, "expires_in": 3600, "token_type": "Bearer"})
}

func (s *Server) gmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "read-only fixture"})
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/")
	switch {
	case p == "profile":
		writeJSON(w, 200, map[string]any{"emailAddress": DemoAccount, "messagesTotal": len(s.Mail)})
	case p == "messages":
		size := s.PageSize
		if n, err := strconv.Atoi(r.URL.Query().Get("maxResults")); err == nil && n > 0 && n < size {
			size = n
		}
		start, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Query().Get("pageToken"), "p"))
		// Gmail lists newest first.
		var ids []map[string]string
		for i := len(s.Mail) - 1 - start; i >= 0 && len(ids) < size; i-- {
			ids = append(ids, map[string]string{"id": s.Mail[i].ID, "threadId": s.Mail[i].Thread})
		}
		out := map[string]any{"messages": ids, "resultSizeEstimate": len(s.Mail)}
		if start+size < len(s.Mail) {
			out["nextPageToken"] = fmt.Sprintf("p%d", start+size)
		}
		writeJSON(w, 200, out)
	case strings.HasPrefix(p, "messages/"):
		id := strings.TrimPrefix(p, "messages/")
		for _, m := range s.Mail {
			if m.ID == id {
				writeJSON(w, 200, m.Gmail())
				return
			}
		}
		writeJSON(w, 404, map[string]string{"error": "not found"})
	default:
		http.NotFound(w, r)
	}
}

var a1Re = regexp.MustCompile(`^(?:'((?:[^']|'')+)'|([^!]+))(?:!([A-Z]+)(\d*)(?::([A-Z]+)(\d*))?)?$`)

func colNum(s string) int {
	n := 0
	for _, c := range s {
		n = n*26 + int(c-'A'+1)
	}
	return n
}

func colName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

func (s *Server) sheets(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/v4/spreadsheets/")
	id, after, _ := strings.Cut(rest, "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	book := s.Sheets[id]
	if book == nil {
		writeJSON(w, 404, map[string]any{"error": map[string]any{"code": 404, "message": "Requested entity was not found."}})
		return
	}
	if after == "" {
		var sheets []map[string]any
		for name := range book {
			sheets = append(sheets, map[string]any{"properties": map[string]any{"title": name}})
		}
		writeJSON(w, 200, map[string]any{"spreadsheetId": id, "sheets": sheets})
		return
	}
	rawRange, _ := url.PathUnescape(strings.TrimPrefix(after, "values/"))
	appendOp := strings.HasSuffix(rawRange, ":append")
	rawRange = strings.TrimSuffix(rawRange, ":append")
	m := a1Re.FindStringSubmatch(rawRange)
	if m == nil {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"code": 400, "message": "Unable to parse range"}})
		return
	}
	tab := strings.ReplaceAll(m[1], "''", "'")
	if tab == "" {
		tab = m[2]
	}
	rows, ok := book[tab]
	if !ok {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"code": 400, "message": "Unable to parse range: " + tab}})
		return
	}
	quoted := "'" + strings.ReplaceAll(tab, "'", "''") + "'"
	switch {
	case r.Method == http.MethodGet:
		s.Reads.Add(1)
		if s.readFault {
			s.readFault = false
			w.Header().Set("Retry-After", "1")
			writeJSON(w, 429, map[string]any{"error": map[string]any{"code": 429}})
			return
		}
		startRow, endRow := 1, len(rows)
		if m[4] != "" {
			startRow, _ = strconv.Atoi(m[4])
		}
		if m[6] != "" {
			endRow, _ = strconv.Atoi(m[6])
		}
		c1, c2 := 1, 1000
		if m[3] != "" {
			c1 = colNum(m[3])
		}
		if m[5] != "" {
			c2 = colNum(m[5])
		}
		var out [][]string
		for i := startRow; i <= endRow && i <= len(rows); i++ {
			row := rows[i-1]
			var cut []string
			for c := c1; c <= c2 && c <= len(row); c++ {
				cut = append(cut, row[c-1])
			}
			for len(cut) > 0 && cut[len(cut)-1] == "" {
				cut = cut[:len(cut)-1]
			}
			out = append(out, cut)
		}
		for len(out) > 0 && len(out[len(out)-1]) == 0 {
			out = out[:len(out)-1]
		}
		if s.fault == FaultBadReadback && len(out) == 1 && startRow > 1 {
			s.fault = FaultNone
			alt := append([]string{}, out[0]...)
			if len(alt) > 0 {
				alt[0] += " (altered)"
			}
			out = [][]string{alt}
		}
		resp := map[string]any{"range": quoted + "!" + rawRange, "majorDimension": "ROWS"}
		if len(out) > 0 {
			resp["values"] = out
		}
		writeJSON(w, 200, resp)
	case r.Method == http.MethodPost && appendOp:
		if r.URL.Query().Get("valueInputOption") != "RAW" {
			writeJSON(w, 400, map[string]any{"error": map[string]any{"code": 400, "message": "fixture requires RAW"}})
			return
		}
		var body struct{ Values [][]string }
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Values) != 1 {
			writeJSON(w, 400, map[string]any{"error": map[string]any{"code": 400}})
			return
		}
		fault := s.fault
		if fault == FaultBeforeWrite {
			s.fault = FaultNone
			writeJSON(w, 500, map[string]any{"error": map[string]any{"code": 500}})
			return
		}
		last := len(rows)
		for last > 0 && strings.Join(rows[last-1], "") == "" {
			last--
		}
		rows = append(rows[:last], append([]string{}, body.Values[0]...))
		book[tab] = rows
		s.Appends.Add(1)
		s.saveLocked()
		n := len(rows)
		updated := fmt.Sprintf("%s!A%d:%s%d", quoted, n, colName(max(1, len(body.Values[0]))), n)
		switch fault {
		case FaultLostResponse:
			s.fault = FaultNone
			if hj, ok := w.(http.Hijacker); ok {
				if c, _, err := hj.Hijack(); err == nil {
					c.Close()
					return
				}
			}
		case FaultNoRange:
			s.fault = FaultNone
			writeJSON(w, 200, map[string]any{"spreadsheetId": id, "updates": map[string]any{}})
			return
		}
		writeJSON(w, 200, map[string]any{"spreadsheetId": id, "tableRange": quoted + "!A1", "updates": map[string]any{"updatedRange": updated, "updatedRows": 1}})
	case r.Method == http.MethodPut:
		var body struct{ Values [][]string }
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Values) != 1 || m[4] == "" {
			writeJSON(w, 400, map[string]any{"error": map[string]any{"code": 400}})
			return
		}
		rowN, _ := strconv.Atoi(m[4])
		for len(rows) < rowN {
			rows = append(rows, []string{})
		}
		start := colNum(m[3])
		row := rows[rowN-1]
		for len(row) < start-1+len(body.Values[0]) {
			row = append(row, "")
		}
		copy(row[start-1:], body.Values[0])
		rows[rowN-1] = row
		book[tab] = rows
		s.saveLocked()
		writeJSON(w, 200, map[string]any{"updatedRange": rawRange})
	default:
		writeJSON(w, 405, map[string]any{"error": "method"})
	}
}
