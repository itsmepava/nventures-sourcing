package discovery_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/discovery"
	"github.com/itsmepava/nventures-sourcing/internal/domain"
	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
)

type memCache struct {
	mu sync.Mutex
	m  map[string]discovery.CacheEntry
}

func (c *memCache) GetPage(_ context.Context, u string) (*discovery.CacheEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[u]; ok {
		return &e, nil
	}
	return nil, nil
}
func (c *memCache) PutPage(_ context.Context, e discovery.CacheEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[e.URL] = e
	return nil
}

func newCrawler(t *testing.T) (*discovery.Crawler, *fixtures.Server, *memCache) {
	srv := fixtures.NewServer("")
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	cache := &memCache{m: map[string]discovery.CacheEntry{}}
	f := &discovery.Fetcher{Cache: cache, Overrides: map[string]string{fixtures.DemoSuffix: srv.Addr}}
	return &discovery.Crawler{F: f, Limits: discovery.DefaultLimits}, srv, cache
}

func TestDiscoverAndQualifyFixturePartners(t *testing.T) {
	c, _, _ := newCrawler(t)
	ctx := context.Background()
	accepted := map[string]bool{}
	allCandidates := map[string]bool{}
	reports := map[string]discovery.PartnerReport{}
	for _, p := range fixtures.Partners() {
		rep := c.DiscoverPartner(ctx, p.Name, p.Website)
		reports[p.Name] = rep
		for _, cand := range rep.Candidates {
			allCandidates[cand.URL] = true
			rec, _ := c.ResearchCompany(ctx, cand.Name, cand.URL)
			reasons := domain.Qualifies(rec)
			if len(reasons) == 0 {
				accepted[cand.URL] = true
				slug := strings.TrimSuffix(strings.TrimPrefix(cand.URL, "https://"), fixtures.DemoSuffix+"/")
				want := map[string]float64{"ledgerleaf": 8e5, "factorysignal": 1.2e6, "cargopilot": 9.5e5, "clinicrelay": 6e5, "routedesk": 2.9e6, "procurepath": 3e6, "fieldorbit": 0, "gridloom": 4e5, "invoicespring": 2.5e5}
				wantHQ := map[string]string{"ledgerleaf": "Sri Lanka", "factorysignal": "India", "cargopilot": "Bangladesh", "clinicrelay": "Nepal", "routedesk": "Pakistan", "procurepath": "Singapore", "fieldorbit": "Bhutan", "gridloom": "Maldives", "invoicespring": "Afghanistan"}
				if rec.TotalFundingUSD == nil || *rec.TotalFundingUSD != want[slug] || rec.HQCountry != wantHQ[slug] {
					t.Errorf("%s: funding %v hq %q", slug, rec.TotalFundingUSD, rec.HQCountry)
				}
				for field, ev := range rec.Evidence {
					for _, e := range ev {
						if !strings.HasPrefix(e.URL, "https://") || e.Quote == "" {
							t.Errorf("%s %s: bad evidence %+v", cand.Name, field, e)
						}
					}
				}
			}
		}
	}
	if len(accepted) != fixtures.EligibleCount {
		t.Errorf("accepted %d want %d: %v", len(accepted), fixtures.EligibleCount, accepted)
	}
	for _, bad := range []string{"overfunded", "outsidehq", "familyplate", "quietco", "oneround", "londonlink"} {
		if accepted["https://"+fixtures.Host(bad)+"/"] {
			t.Errorf("%s accepted", bad)
		}
		if !allCandidates["https://"+fixtures.Host(bad)+"/"] {
			t.Errorf("%s not discovered", bad)
		}
	}
	if !strings.Contains(strings.Join(reports["Himalaya Catalyst (demo)"].Warnings, " "), "JavaScript") {
		t.Errorf("JS-only not flagged: %v", reports["Himalaya Catalyst (demo)"].Warnings)
	}
	closed := reports["Closed Door Angels (demo)"]
	if len(closed.Candidates) != 0 || closed.Sources[0].Status != "robots" {
		t.Errorf("robots not honoured: %+v", closed.Sources)
	}
	if len(reports["Karachi Angel Forum (demo)"].Warnings) == 0 {
		t.Error("missing website not reported")
	}
	lanka := reports["Lanka Seed Circle (demo)"]
	for _, s := range lanka.Sources {
		if strings.Contains(s.URL, "/private/") {
			t.Error("robots-disallowed page fetched")
		}
	}
	for _, cand := range lanka.Candidates {
		if strings.Contains(cand.URL, "169.254") || strings.Contains(cand.URL, "127.0.0.1") || strings.Contains(cand.URL, "linkedin") {
			t.Errorf("unsafe/social candidate %s", cand.URL)
		}
	}
	if !allCandidates["https://"+fixtures.Host("factorysignal")+"/"] {
		t.Error("pagination not followed")
	}
	if !allCandidates["https://"+fixtures.Host("clinicrelay")+"/"] {
		t.Error("RSS feed not followed")
	}
}

func TestRateLimitRetryAndCache(t *testing.T) {
	c, srv, _ := newCrawler(t)
	ctx := context.Background()
	start := time.Now()
	r := c.F.Fetch(ctx, "https://"+fixtures.Host("indusventures")+"/portfolio")
	if r.Status != "ok" || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("429 not retried with Retry-After: %s %s", r.Status, time.Since(start))
	}
	before := srv.Requests.Load()
	r = c.F.Fetch(ctx, "https://"+fixtures.Host("indusventures")+"/portfolio")
	if r.Status != "cached" || srv.Requests.Load() != before {
		t.Fatalf("cache miss: %s", r.Status)
	}
	if c.F.Counters.Snapshot()["cache_hits"] < 1 {
		t.Error("cache hit not counted")
	}
}

func TestConditionalRevalidation(t *testing.T) {
	c, _, cache := newCrawler(t)
	ctx := context.Background()
	u := "https://" + fixtures.Host("ledgerleaf") + "/about"
	c.F.Fetch(ctx, u)
	e := cache.m[u]
	e.FetchedAt = time.Now().Add(-48 * time.Hour)
	cache.m[u] = e
	if r := c.F.Fetch(ctx, u); r.Status != "cached" || r.Doc == nil {
		t.Fatalf("304 revalidation: %s", r.Status)
	}
}

func TestSSRFProtections(t *testing.T) {
	f := &discovery.Fetcher{}
	ctx := context.Background()
	for _, u := range []string{"http://127.0.0.1/", "http://169.254.169.254/latest/meta-data/", "http://[::1]/", "http://10.0.0.5/", "http://localhost/", "file:///etc/passwd", "http://metadata.google.internal/", "gopher://example.com/", "http://user:pw@example.com/"} {
		if r := f.Fetch(ctx, u); r.Status != "blocked" {
			t.Errorf("%s: %s", u, r.Status)
		}
	}
	// A DNS answer pointing at loopback is refused at connect time. This needs a
	// resolver that answers "localhost." (some sandboxes resolve nothing); the
	// redirect case below covers the same dial-time guard without DNS.
	if addrs, err := net.LookupHost("localhost."); err != nil || len(addrs) == 0 {
		t.Logf("resolver cannot answer localhost. (%v); DNS-loopback sub-check not applicable", err)
	} else if r := f.Fetch(ctx, "http://localhost.:80/"); r.Status != "blocked" && r.Status != "robots" {
		t.Errorf("private DNS answer allowed: %s %s", r.Status, r.Note)
	}
	// A redirect from an allowed host to a private address is refused.
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) }))
	defer local.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(404)
			return
		}
		http.Redirect(w, r, "http://127.0.0.1:80/admin", http.StatusFound)
	}))
	defer redir.Close()
	g := &discovery.Fetcher{Overrides: map[string]string{".redirect-demo.test": strings.TrimPrefix(redir.URL, "http://")}}
	if r := g.Fetch(ctx, "http://site.redirect-demo.test/"); r.Status != "blocked" {
		t.Errorf("redirect to private allowed: %s %s", r.Status, r.Note)
	}
	for _, a := range []string{"100.64.1.1", "192.168.1.1", "fd00::1", "::ffff:127.0.0.1", "0.0.0.0"} {
		if discovery.PublicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s considered public", a)
		}
	}
	if !discovery.PublicAddr(netip.MustParseAddr("8.8.8.8")) {
		t.Error("public address refused")
	}
}

func TestSizeMIMEAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(404)
		case "/big":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<p>" + strings.Repeat("a", 5000) + "</p>"))
		case "/bin":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0, 1, 2})
		case "/slow":
			time.Sleep(2 * time.Second)
		}
	}))
	defer srv.Close()
	f := &discovery.Fetcher{MaxBytes: 1000, Overrides: map[string]string{".size-demo.test": strings.TrimPrefix(srv.URL, "http://")}}
	ctx := context.Background()
	if r := f.Fetch(ctx, "http://a.size-demo.test/big"); r.Status != "truncated" || len(r.Body) != 1000 {
		t.Errorf("size limit: %s %d", r.Status, len(r.Body))
	}
	if r := f.Fetch(ctx, "http://a.size-demo.test/bin"); r.Status != "mime" {
		t.Errorf("mime: %s", r.Status)
	}
	cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if r := f.Fetch(cctx, "http://a.size-demo.test/slow"); r.Status != "error" || time.Since(start) > time.Second {
		t.Errorf("cancel: %s %s", r.Status, time.Since(start))
	}
}

// A partner list may give the portfolio page itself (a "Company Portfolio"
// column) rather than the home page; that page is the company list even when
// its path doesn't look like one.
func TestDeepLinkPortfolioSeed(t *testing.T) {
	c, _, _ := newCrawler(t)
	rep := c.DiscoverPartner(context.Background(), "Lanka Seed Circle (demo)", "https://lankaseed"+fixtures.DemoSuffix+"/our-picks")
	got := map[string]bool{}
	for _, cand := range rep.Candidates {
		if strings.HasSuffix(cand.FoundOn, "/our-picks") {
			got[strings.TrimSuffix(strings.TrimPrefix(cand.URL, "https://"), fixtures.DemoSuffix+"/")] = true
		}
	}
	// The deep link's own companies are found on it (the sitemap's main
	// portfolio is still visited too, for coverage).
	if !got["ledgerleaf"] || !got["factorysignal"] {
		t.Fatalf("candidates from deep link: %+v warnings %v", rep.Candidates, rep.Warnings)
	}
}
