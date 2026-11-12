package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/discovery"
	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
	"github.com/itsmepava/nventures-sourcing/internal/google"
	"github.com/itsmepava/nventures-sourcing/internal/jobs"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

type cacheAdapter struct{ s *store.Store }

func (c cacheAdapter) GetPage(ctx context.Context, u string) (*discovery.CacheEntry, error) {
	r, err := c.s.GetCache(ctx, u)
	if r == nil || err != nil {
		return nil, err
	}
	return &discovery.CacheEntry{URL: r.URL, Status: r.Status, ContentType: r.ContentType, ETag: r.ETag, LastModified: r.LastModified, Body: r.Body, Truncated: r.Truncated, FetchedAt: r.FetchedAt}, nil
}
func (c cacheAdapter) PutPage(ctx context.Context, e discovery.CacheEntry) error {
	return c.s.PutCache(ctx, store.CacheRow{URL: e.URL, Status: e.Status, ContentType: e.ContentType, ETag: e.ETag, LastModified: e.LastModified, Body: e.Body, Truncated: e.Truncated, FetchedAt: e.FetchedAt})
}

type harness struct {
	st  *store.Store
	fx  *fixtures.Server
	mgr *jobs.Manager
	cfg jobs.Config
	mu  sync.Mutex
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fx := fixtures.NewServer(filepath.Join(dir, "sheets.json"))
	if err := fx.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fx.Close)
	h := &harness{st: st, fx: fx, cfg: jobs.Config{Mode: "demo", ExpectedAccount: fixtures.DemoAccount, InboundSheet: fixtures.InboundSheetID, InboundTab: "Pitches", OutboundSheet: fixtures.OutboundSheetID, OutboundTab: "Active Sourcing", PartnerTab: "Partner List", ControlTab: "Sourcing Control", MaxResearch: 45}}
	hc := &http.Client{Timeout: 5 * time.Second}
	tokens := google.RefreshTokenSource(hc, fx.URL+"/token", fixtures.DemoClientID, "", fixtures.DemoRefresh)
	fast := netx.Policy{Attempts: 3, MaxSleep: 2 * time.Second}
	crawler := &discovery.Crawler{F: &discovery.Fetcher{Cache: cacheAdapter{st}, Overrides: map[string]string{fixtures.DemoSuffix: fx.Addr}}, Limits: discovery.DefaultLimits}
	h.mgr = jobs.NewManager(jobs.Env{Store: st, Crawler: crawler, Owner: "test",
		Config:  func(context.Context) jobs.Config { h.mu.Lock(); defer h.mu.Unlock(); return h.cfg },
		Connect: func(context.Context) (*jobs.Conn, error) { return &jobs.Conn{Gmail: &google.Gmail{Base: fx.URL, HTTP: hc, Tokens: tokens, Policy: fast}, Sheets: &google.Sheets{Base: fx.URL, HTTP: hc, Tokens: tokens, Policy: fast}}, nil }})
	return h
}

func (h *harness) run(t *testing.T, kind string, params map[string]any) store.Job {
	t.Helper()
	j, err := h.mgr.Start(context.Background(), kind, params)
	if err != nil {
		t.Fatal(err)
	}
	h.mgr.Wait()
	j, _ = h.st.Job(context.Background(), j.ID)
	return j
}

func count(j store.Job, k string) int {
	f, _ := j.Counters[k].(float64)
	return int(f)
}

func pitchesByName(t *testing.T, st *store.Store) map[string]store.Pitch {
	ps, _, err := st.Pitches(context.Background(), store.ListQuery{PerPage: 500})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.Pitch{}
	for _, p := range ps {
		out[p.Name] = p
	}
	return out
}

func TestInboundImportSyncAndRepeat(t *testing.T) {
	h := newHarness(t)
	j := h.run(t, "inbound_import", map[string]any{"limit": float64(100), "sync": true})
	if j.Status != "completed" {
		t.Fatalf("status %s: %s", j.Status, j.Message)
	}
	ps := pitchesByName(t, h.st)
	if len(ps) < 14 {
		t.Fatalf("expected every distinct message imported, got %d", len(ps))
	}
	if ps["Kelp Ledger (demo)"].Status != "verified" || ps["Orbit Pantry (demo)"].Status != "verified" || ps["Harbor Lens (demo)"].Status != "verified" {
		t.Error("ready pitches (incl. same thread / same founder) not verified")
	}
	rows := h.fx.Rows(fixtures.InboundSheetID, "Pitches")
	names := map[string]int{}
	for _, r := range rows[1:] {
		names[r[0]]++
		if len(r) > 4 && r[4] != "" {
			t.Errorf("Status cell written: %v", r)
		}
	}
	for n, c := range names {
		if c > 1 {
			t.Errorf("%s written %d times", n, c)
		}
	}
	if names["Meridian Holdings (demo)"] != 0 || names["Test Company"] != 0 || names["Lumen Freight (demo)"] != 0 {
		t.Error("review/skipped pitch written without approval")
	}
	if names["=Sum Labs (demo) — කෘෂි"] != 1 {
		t.Errorf("unicode/formula name not preserved: %v", names)
	}
	var sum extract.Pitch
	_ = json.Unmarshal(ps["=Sum Labs (demo) — කෘෂි"].Data, &sum)
	for _, r := range rows {
		if r[0] == "=Sum Labs (demo) — කෘෂි" && !strings.Contains(r[1], "\n") {
			t.Error("newline lost in RAW write")
		}
	}
	if ps["Kelp Ledger (demo)"].Status == "duplicate" {
		t.Error("original marked duplicate")
	}
	_, dups, _ := h.st.Pitches(context.Background(), store.ListQuery{Status: "duplicate"})
	if dups != 1 {
		t.Errorf("expected one duplicate re-submission, got %d", dups)
	}
	appends := h.fx.Appends.Load()
	j2 := h.run(t, "inbound_import", map[string]any{"limit": float64(100), "sync": true})
	if count(j2, "imported") != 0 || h.fx.Appends.Load() != appends {
		t.Fatalf("repeat import duplicated work: %+v appends %d→%d", j2.Counters, appends, h.fx.Appends.Load())
	}
}

func TestPaginationLimitAndResume(t *testing.T) {
	h := newHarness(t)
	j := h.run(t, "inbound_import", map[string]any{"limit": float64(4)})
	if count(j, "imported") != 4 || j.Counters["more_available"] != true {
		t.Fatalf("limit: %+v", j.Counters)
	}
	total := 4
	for i := 0; i < 5; i++ {
		j = h.run(t, "inbound_import", map[string]any{"limit": float64(4)})
		total += count(j, "imported")
	}
	if total != len(fixtures.Mailbox()) {
		t.Fatalf("resume lost or duplicated messages: %d of %d", total, len(fixtures.Mailbox()))
	}
	if j.Counters["more_available"] == true {
		t.Error("more_available after exhausting mailbox")
	}
}

func TestApproveThenSyncNowWithoutRescan(t *testing.T) {
	h := newHarness(t)
	h.run(t, "inbound_import", map[string]any{"limit": float64(100)})
	p := pitchesByName(t, h.st)["Tidewell (demo)"]
	if p.Status != "review" {
		t.Fatalf("status %s", p.Status)
	}
	var d extract.Pitch
	_ = json.Unmarshal(p.Data, &d)
	d.Description = "Cold-chain monitoring sensors and software for seafood exporters in Bangladesh."
	d.HQ = "Dhaka, Bangladesh (founder-reported)"
	p.Data, _ = json.Marshal(d)
	p.Status = "ready"
	if err := h.st.UpdatePitch(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	before := h.fx.Requests.Load()
	j := h.run(t, "inbound_sync", map[string]any{"ids": []any{float64(p.ID)}})
	if count(j, "verified") != 1 {
		t.Fatalf("%+v %s", j.Counters, j.Message)
	}
	// token + read snapshot + append + readback; no Gmail requests.
	if h.fx.Requests.Load()-before > 5 {
		t.Errorf("sync rescanned Gmail: %d requests", h.fx.Requests.Load()-before)
	}
	got, _ := h.st.Pitch(context.Background(), p.ID)
	if got.Status != "verified" {
		t.Error(got.Status)
	}
	rows := h.fx.Rows(fixtures.InboundSheetID, "Pitches")
	if rows[len(rows)-1][2] != "Dhaka, Bangladesh (founder-reported)" {
		t.Error("edit not persisted before sync")
	}
}

func syncOne(t *testing.T, h *harness, name string) (store.Job, store.Pitch) {
	p := pitchesByName(t, h.st)[name]
	j := h.run(t, "inbound_sync", map[string]any{"ids": []any{float64(p.ID)}})
	got, _ := h.st.Pitch(context.Background(), p.ID)
	return j, got
}

func TestSyncFaultsAreReconciledNeverBlindlyRetried(t *testing.T) {
	h := newHarness(t)
	h.run(t, "inbound_import", map[string]any{"limit": float64(100)})
	cases := []struct {
		fault      fixtures.Fault
		name       string
		first      string
		appendedBy int64
	}{
		{fixtures.FaultLostResponse, "Kelp Ledger (demo)", "uncertain", 1},
		{fixtures.FaultBeforeWrite, "Orbit Pantry (demo)", "uncertain", 0},
		{fixtures.FaultNoRange, "Solace Grid (demo)", "uncertain", 1},
		{fixtures.FaultBadReadback, "Mosaic Clinic (demo)", "conflict", 1},
	}
	for _, c := range cases {
		h.fx.SetFault(c.fault)
		before := h.fx.Appends.Load()
		_, p := syncOne(t, h, c.name)
		if p.Status != c.first || h.fx.Appends.Load()-before != c.appendedBy {
			t.Errorf("%s first: status %s appends %d", c.fault, p.Status, h.fx.Appends.Load()-before)
		}
		mid := h.fx.Appends.Load()
		_, p = syncOne(t, h, c.name)
		rows := h.fx.Rows(fixtures.InboundSheetID, "Pitches")
		n := 0
		for _, r := range rows {
			if r[0] == c.name {
				n++
			}
		}
		switch c.fault {
		case fixtures.FaultBadReadback:
			if p.Status != "verified" || h.fx.Appends.Load() != mid {
				t.Errorf("readback: second %s, appended %d", p.Status, h.fx.Appends.Load()-mid)
			}
		case fixtures.FaultBeforeWrite:
			if p.Status != "verified" || n != 1 {
				t.Errorf("500: second %s rows %d", p.Status, n)
			}
		default:
			if p.Status != "verified" || n != 1 || h.fx.Appends.Load() != mid {
				t.Errorf("%s: second %s rows %d appended %d", c.fault, p.Status, n, h.fx.Appends.Load()-mid)
			}
		}
	}
}

func TestExistingRowAndManualStatusNeverOverwritten(t *testing.T) {
	h := newHarness(t)
	h.fx.SetRows(fixtures.InboundSheetID, "Pitches", [][]string{google.InboundHeaders, {"Kelp Ledger (demo)", "Old description", "", "", "Passed", "Manual notes"}})
	h.run(t, "inbound_import", map[string]any{"limit": float64(100), "sync": true})
	rows := h.fx.Rows(fixtures.InboundSheetID, "Pitches")
	if rows[1][4] != "Passed" || rows[1][1] != "Old description" {
		t.Fatal("manual row changed")
	}
	if pitchesByName(t, h.st)["Kelp Ledger (demo)"].Status != "already_exists" {
		t.Error("existing identity not detected")
	}
}

func TestMissingDestinationAndBadHeadersWriteNothing(t *testing.T) {
	h := newHarness(t)
	h.run(t, "inbound_import", map[string]any{"limit": float64(100)})
	h.mu.Lock()
	h.cfg.InboundSheet = ""
	h.mu.Unlock()
	before := h.fx.Requests.Load()
	j := h.run(t, "inbound_sync", nil)
	if j.Status != "failed" || h.fx.Requests.Load() != before {
		t.Fatalf("%s %d", j.Status, h.fx.Requests.Load()-before)
	}
	h.mu.Lock()
	h.cfg.InboundSheet = fixtures.InboundSheetID
	h.mu.Unlock()
	h.fx.SetRows(fixtures.InboundSheetID, "Pitches", [][]string{{"Name", "Desc"}})
	j = h.run(t, "inbound_sync", nil)
	if j.Status != "failed" || h.fx.Appends.Load() != 0 {
		t.Fatalf("bad headers: %s appends %d", j.Status, h.fx.Appends.Load())
	}
}

func TestOutboundRunSyncRotationAndRepeat(t *testing.T) {
	h := newHarness(t)
	j := h.run(t, "outbound_run", map[string]any{"target": float64(10), "max_partners": float64(10), "sync": true})
	if j.Status != "partial" || count(j, "accepted") != fixtures.EligibleCount || count(j, "verified") != fixtures.EligibleCount {
		t.Fatalf("%s %s %+v", j.Status, j.Message, j.Counters)
	}
	if !strings.Contains(j.Message, "not relaxed") {
		t.Error("partial outcome not explained")
	}
	rows := h.fx.Rows(fixtures.OutboundSheetID, "Active Sourcing")
	if len(rows) != 1+fixtures.EligibleCount || len(rows[1]) != 38 {
		t.Fatalf("rows %d width %d", len(rows), len(rows[1]))
	}
	for _, r := range rows[1:] {
		if r[25] != "Sourced" || r[28] == "" || r[30] == "" || r[31] != "" {
			t.Errorf("38-column defaults wrong: status=%q notes=%q/%q analyst=%q", r[25], r[28], r[30], r[31])
		}
	}
	partners, _ := h.st.Partners(context.Background(), false, 0)
	for _, p := range partners {
		if p.Relevant && p.RunCount != 1 {
			t.Errorf("%s run count %d", p.Name, p.RunCount)
		}
		if p.Name == "Irrelevant Fund (demo)" && p.Relevant {
			t.Error("relevance not imported")
		}
	}
	ctl := h.fx.Rows(fixtures.OutboundSheetID, "Sourcing Control")
	if len(ctl) < 6 {
		t.Errorf("control tab not updated: %d rows", len(ctl))
	}
	cands, _ := h.st.Candidates(context.Background(), j.ID, "", 500)
	outcomes := map[string]int{}
	for _, c := range cands {
		outcomes[c.Outcome]++
	}
	if outcomes["rejected"] < 4 || outcomes["needs_research"] < 2 || outcomes["duplicate"] < 1 {
		t.Errorf("outcomes %v", outcomes)
	}
	appends := h.fx.Appends.Load()
	j2 := h.run(t, "outbound_run", map[string]any{"target": float64(10), "max_partners": float64(10), "sync": true})
	if count(j2, "accepted") != 0 || h.fx.Appends.Load()-appends > 6 {
		t.Fatalf("repeat run added companies/rows: %+v (appends %d)", j2.Counters, h.fx.Appends.Load()-appends)
	}
	rows = h.fx.Rows(fixtures.OutboundSheetID, "Active Sourcing")
	if len(rows) != 1+fixtures.EligibleCount {
		t.Fatal("duplicate outbound rows")
	}
	partners, _ = h.st.Partners(context.Background(), true, 0)
	for _, p := range partners {
		if p.RunCount != 2 {
			t.Errorf("%s run count %d after two runs", p.Name, p.RunCount)
		}
	}
}

func TestTargetIsNeverRelaxedAndLocalOnlyRun(t *testing.T) {
	h := newHarness(t)
	h.mu.Lock()
	h.cfg.OutboundSheet = ""
	h.mu.Unlock()
	_, _ = h.st.AddPartner(context.Background(), "Lanka Seed Circle (demo)", "https://"+fixtures.Host("lankaseed"), true)
	j := h.run(t, "outbound_run", map[string]any{"target": float64(2), "max_partners": float64(1)})
	if count(j, "accepted") != 2 || j.Status != "completed" {
		t.Fatalf("%s %+v", j.Status, j.Counters)
	}
	comps, _ := h.st.AllCompanies(context.Background(), "")
	for _, c := range comps {
		if c.SyncStatus != "pending" {
			t.Error("local-only run synced")
		}
	}
	if h.fx.Appends.Load() != 0 {
		t.Error("local-only run wrote to sheets")
	}
}

func TestConcurrentStartsSingleWriter(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	var busy, ok int
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.mgr.Start(context.Background(), "inbound_import", map[string]any{"limit": float64(100)})
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(err, jobs.ErrBusy) {
				busy++
			} else if err == nil {
				ok++
			}
		}()
	}
	wg.Wait()
	h.mgr.Wait()
	if ok != 1 || busy != 7 {
		t.Fatalf("ok=%d busy=%d", ok, busy)
	}
}

func TestCancellationAndRestartRecovery(t *testing.T) {
	h := newHarness(t)
	j, err := h.mgr.Start(context.Background(), "outbound_run", map[string]any{"target": float64(50), "max_partners": float64(10)})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = h.mgr.Cancel(context.Background(), j.ID)
	h.mgr.Wait()
	j, _ = h.st.Job(context.Background(), j.ID)
	if j.Status != "cancelled" {
		t.Fatalf("status %s", j.Status)
	}
	// Simulate a crash mid-job and mid-write.
	ctx := context.Background()
	crashed, _ := h.st.CreateJob(ctx, "inbound_sync", "demo", nil)
	op, _ := h.st.BeginSyncOp(ctx, "pitch", 999, "x", []string{"a"}, crashed.ID)
	_ = h.st.MarkSyncOp(ctx, op.ID, "writing", 0, "", "", true)
	if n, _ := h.st.InterruptRunning(ctx); n != 1 {
		t.Fatalf("interrupted %d", n)
	}
	if n, _ := h.st.RecoverSyncOps(ctx); n != 1 {
		t.Fatalf("recovered %d", n)
	}
	got, _ := h.st.Job(ctx, crashed.ID)
	o, _ := h.st.LatestSyncOp(ctx, "pitch", 999)
	if got.Status != "interrupted" || o.Status != "uncertain" {
		t.Fatalf("%s %s", got.Status, o.Status)
	}
}

func TestRateLimitedSheetReadIsRetried(t *testing.T) {
	h := newHarness(t)
	h.run(t, "inbound_import", map[string]any{"limit": float64(100)})
	h.fx.SetFault(fixtures.FaultRateLimitRead)
	j := h.run(t, "inbound_sync", nil)
	if j.Status != "completed" || count(j, "verified") == 0 {
		t.Fatalf("%s %s", j.Status, j.Message)
	}
}
