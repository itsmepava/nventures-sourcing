package httpapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/config"
	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
)

// RehearsalReport is the outcome of the deterministic presentation rehearsal.
type RehearsalReport struct {
	Steps        []string         `json:"steps"`
	Inbound      map[string]any   `json:"inbound"`
	Outbound     map[string]any   `json:"outbound"`
	Repeat       map[string]any   `json:"repeat"`
	InboundRows  int              `json:"inbound_sheet_rows"`
	OutboundRows int              `json:"outbound_sheet_rows"`
	Crawler      map[string]int64 `json:"crawler"`
	Passed       bool             `json:"passed"`
	Failures     []string         `json:"failures"`
}

// Rehearse runs both workflows twice through the real HTTP API against the
// local fixture server, with no external credentials and no paid adapters.
func Rehearse(dataDir, version string, log *slog.Logger) (RehearsalReport, error) {
	rep := RehearsalReport{}
	cfg := config.Config{Mode: "demo", DataDir: dataDir, Host: "127.0.0.1", MaxResearch: 45, NoBrowser: true}
	app, err := New(cfg, version, log)
	if err != nil {
		return rep, err
	}
	defer app.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return rep, err
	}
	app.Port = ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	base := fmt.Sprintf("http://127.0.0.1:%d", app.Port)
	hc := &http.Client{Timeout: 30 * time.Second}
	call := func(method, path string, body any, out any) (int, error) {
		var buf io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			buf = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, base+path, buf)
		req.Header.Set("X-NV-Request", "1")
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		if out != nil {
			_ = json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode, nil
	}
	wait := func(id float64) map[string]any {
		deadline := time.Now().Add(3 * time.Minute)
		for time.Now().Before(deadline) {
			var out struct {
				Job map[string]any `json:"job"`
			}
			if _, err := call("GET", fmt.Sprintf("/api/jobs/%.0f", id), nil, &out); err == nil && out.Job["status"] != "running" {
				return out.Job
			}
			time.Sleep(150 * time.Millisecond)
		}
		return map[string]any{"status": "timeout"}
	}
	start := func(path string, body map[string]any) map[string]any {
		var job map[string]any
		code, err := call("POST", path, body, &job)
		if err != nil || code != 202 {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s returned %d %v", path, code, job))
			return map[string]any{}
		}
		return wait(job["id"].(float64))
	}
	fail := func(cond bool, msg string) {
		if !cond {
			rep.Failures = append(rep.Failures, msg)
		}
	}
	num := func(j map[string]any, k string) int {
		c, _ := j["counters"].(map[string]any)
		f, _ := c[k].(float64)
		return int(f)
	}
	var health map[string]any
	code, _ := call("GET", "/health", nil, &health)
	fail(code == 200 && health["mode"] == "demo", "health check")
	rep.Steps = append(rep.Steps, "Health OK (demo mode, fixture Gmail/Sheets/websites on loopback)")

	in := start("/api/inbox/import", map[string]any{"limit": 100, "sync": true})
	rep.Inbound = in
	fail(in["status"] == "completed", fmt.Sprintf("inbound import status %v: %v", in["status"], in["message"]))
	fail(num(in, "imported") == len(fixtures.Mailbox()), "every fixture message imported once")
	fail(num(in, "verified") == 8 && num(in, "review") == 4 && num(in, "skipped") == 3 && num(in, "duplicates") == 1, fmt.Sprintf("inbound triage counts: %v", in["counters"]))
	rep.Steps = append(rep.Steps, fmt.Sprintf("Inbox import & sync: %v", in["message"]))

	// Approve one pitch from review and sync it immediately without rescanning.
	var list struct {
		Items []map[string]any `json:"items"`
	}
	call("GET", "/api/pitches?status=review&q=Tidewell", nil, &list)
	if len(list.Items) == 1 {
		id := list.Items[0]["id"].(float64)
		var data map[string]any
		_ = json.Unmarshal([]byte(mustJSON(list.Items[0]["data"])), &data)
		code, _ := call("POST", fmt.Sprintf("/api/pitches/%.0f", id), map[string]any{"name": data["name"], "description": "Cold-chain monitoring sensors and software for seafood exporters.", "hq": "", "contact": data["contact"], "website": data["website"], "notes": data["notes"], "decision": "ready"}, nil)
		fail(code == 200, "approve reviewed pitch")
		s := start("/api/inbox/sync", map[string]any{"ids": []float64{id}})
		fail(num(s, "verified") == 1, "selected pitch synced now")
		rep.Steps = append(rep.Steps, "Reviewed pitch approved and synced immediately (no Gmail rescan)")
	} else {
		fail(false, "review pitch for approval not found")
	}

	out := start("/api/sourcing/run", map[string]any{"target": 10, "max_partners": 10, "sync": true})
	rep.Outbound = out
	fail(num(out, "accepted") == fixtures.EligibleCount, fmt.Sprintf("outbound accepted %d want %d", num(out, "accepted"), fixtures.EligibleCount))
	fail(num(out, "verified") == fixtures.EligibleCount, "outbound rows verified")
	fail(out["status"] == "partial", "target 10 with 9 eligible must be an honest partial outcome")
	rep.Steps = append(rep.Steps, fmt.Sprintf("Sourcing run: %v", out["message"]))

	inRows := len(app.Fixture.Rows(fixtures.InboundSheetID, "Pitches"))
	outRows := len(app.Fixture.Rows(fixtures.OutboundSheetID, "Active Sourcing"))
	in2 := start("/api/inbox/import", map[string]any{"limit": 100, "sync": true})
	out2 := start("/api/sourcing/run", map[string]any{"target": 10, "max_partners": 10, "sync": true})
	rep.Repeat = map[string]any{"inbound": in2["message"], "outbound": out2["message"]}
	fail(num(in2, "imported") == 0 && num(out2, "accepted") == 0, "repeat produced new records")
	fail(len(app.Fixture.Rows(fixtures.InboundSheetID, "Pitches")) == inRows && len(app.Fixture.Rows(fixtures.OutboundSheetID, "Active Sourcing")) == outRows, "repeat produced duplicate sheet rows")
	rep.Steps = append(rep.Steps, "Repeat of both workflows: zero new companies, zero new sheet rows")
	rep.InboundRows, rep.OutboundRows = inRows-1, outRows-1
	rep.Crawler = app.Crawler.F.Counters.Snapshot()
	var csvResp *http.Response
	if req, err := http.NewRequest("GET", base+"/api/companies/export.csv", nil); err == nil {
		csvResp, err = hc.Do(req)
		fail(err == nil && csvResp.StatusCode == 200, "CSV export")
		if csvResp != nil {
			csvResp.Body.Close()
		}
	}
	rep.Passed = len(rep.Failures) == 0
	_ = context.Background()
	return rep, nil
}

func mustJSON(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
