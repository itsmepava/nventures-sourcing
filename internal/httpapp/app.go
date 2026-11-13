// Package httpapp wires configuration, storage, jobs and Google access into
// the local HTTP application.
package httpapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/config"
	"github.com/itsmepava/nventures-sourcing/internal/discovery"
	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
	"github.com/itsmepava/nventures-sourcing/internal/google"
	"github.com/itsmepava/nventures-sourcing/internal/jobs"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

// App is the running application.
type App struct {
	Cfg           config.Config
	Version       string
	Store         *store.Store
	Jobs          *jobs.Manager
	Fixture       *fixtures.Server
	Crawler       *discovery.Crawler
	Log           *slog.Logger
	Started       time.Time
	Port          int // actual listening port
	owner         string
	hc            *http.Client
	stopBeat      context.CancelFunc
	stopSched     chan struct{}
	schedulerTick time.Duration
	paidSearches  atomic.Int64 // opt-in Tavily requests since start
	searchBase    string       // Tavily API base (tests point this at a fake)

	oauthMu sync.Mutex
	pending map[string]google.PKCE
}

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

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ErrInstanceRunning means another process holds this data directory.
var ErrInstanceRunning = errors.New("another nVentures process is already using this data directory; open it in the browser or stop it first")

// New opens storage, recovers interrupted work and prepares services.
func New(cfg config.Config, version string, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(cfg.ModeDir(), 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	a := &App{Cfg: cfg, Version: version, Store: st, Log: log, Started: time.Now(), owner: "proc-" + randomID(), hc: &http.Client{Timeout: 60 * time.Second}, pending: map[string]google.PKCE{}}
	ctx := context.Background()
	ok, err := st.AcquireLease(ctx, "instance", a.owner, 0, 30*time.Second)
	if err != nil || !ok {
		st.Close()
		if err == nil {
			err = ErrInstanceRunning
		}
		return nil, err
	}
	beat, cancel := context.WithCancel(context.Background())
	a.stopBeat = cancel
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-beat.Done():
				return
			case <-t.C:
				_ = st.RenewLease(context.Background(), "instance", a.owner, 30*time.Second)
			}
		}
	}()
	_ = st.ClearLeasesExcept(ctx, "instance", a.owner)
	if n, _ := st.InterruptRunning(ctx); n > 0 {
		st.Audit(ctx, 0, "interrupted", fmt.Sprintf("%d job(s) were interrupted by the previous shutdown; saved work was kept", n), nil)
	}
	if n, _ := st.RecoverSyncOps(ctx); n > 0 {
		st.Audit(ctx, 0, "sync_recovery", fmt.Sprintf("%d sheet write(s) were interrupted; they will be reconciled by reading the sheet before any retry", n), nil)
	}
	if err := a.modeGuard(ctx); err != nil {
		a.Close()
		return nil, err
	}
	fetcher := &discovery.Fetcher{Cache: cacheAdapter{st}, MinInterval: time.Second}
	if cfg.Mode == "demo" {
		a.Fixture = fixtures.NewServer(filepath.Join(cfg.ModeDir(), "demo-sheets.json"))
		if err := a.Fixture.Start(); err != nil {
			a.Close()
			return nil, err
		}
		fetcher.Overrides = map[string]string{fixtures.DemoSuffix: a.Fixture.Addr}
		fetcher.MinInterval = 0
		a.seedDemo(ctx)
	}
	a.Crawler = &discovery.Crawler{F: fetcher, Limits: discovery.DefaultLimits}
	a.Jobs = jobs.NewManager(jobs.Env{Store: st, Crawler: a.Crawler, Sites: a.SiteFinder, Config: a.JobConfig, Connect: a.Connect, Owner: a.owner, Log: log, OnFinish: a.notifyJobFinished})
	a.schedulerTick = 20 * time.Second
	a.stopSched = make(chan struct{})
	return a, nil
}

// StartScheduler begins running due schedules in the background.
func (a *App) StartScheduler() {
	go a.scheduler(a.stopSched)
}

// Close stops background work and releases the data directory.
func (a *App) Close() {
	if a.stopSched != nil {
		select {
		case <-a.stopSched:
		default:
			close(a.stopSched)
		}
	}
	if a.Jobs != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		a.Jobs.Shutdown(ctx)
		cancel()
	}
	if a.stopBeat != nil {
		a.stopBeat()
	}
	if a.Fixture != nil {
		a.Fixture.Close()
	}
	_ = a.Store.ReleaseLease(context.Background(), "instance", a.owner)
	a.Store.Close()
}

func (a *App) modeGuard(ctx context.Context) error {
	var wrong int
	want := 0
	if a.Cfg.Mode == "demo" {
		want = 1
	}
	_ = a.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companies WHERE demo<>?`, want).Scan(&wrong)
	if wrong > 0 {
		return fmt.Errorf("database %s contains records from another mode; demo and live data must stay separate", a.Cfg.DBPath())
	}
	return nil
}

func (a *App) seedDemo(ctx context.Context) {
	if ps, _ := a.Store.Partners(ctx, false, 0); len(ps) == 0 {
		for _, p := range fixtures.Partners() {
			_ = a.Store.MergePartner(ctx, p.Name, p.Website, true, "", 0, "") // as if read from the demo partner list
		}
	}
}

// setting returns a DB override or the configured default.
func (a *App) setting(ctx context.Context, key, def string) string {
	return a.Store.GetSetting(ctx, "cfg."+key, def)
}

// JobConfig is the effective configuration for jobs.
func (a *App) JobConfig(ctx context.Context) jobs.Config {
	c := jobs.Config{Mode: a.Cfg.Mode, MaxResearch: a.Cfg.MaxResearch}
	if a.Cfg.Mode == "demo" {
		c.ExpectedAccount = fixtures.DemoAccount
		c.InboundSheet, c.InboundTab = fixtures.InboundSheetID, "Pitches"
		c.OutboundSheet, c.OutboundTab, c.PartnerTab, c.ControlTab = fixtures.OutboundSheetID, "Active Sourcing", "Partner List", "Sourcing Control"
		return c
	}
	c.ExpectedAccount = strings.ToLower(a.setting(ctx, "expected_account", a.Cfg.ExpectedAccount))
	c.GmailQuery = a.setting(ctx, "gmail_query", a.Cfg.GmailQuery)
	c.InboundSheet = a.setting(ctx, "inbound_sheet", a.Cfg.InboundSheet)
	c.InboundTab = a.setting(ctx, "inbound_tab", a.Cfg.InboundTab)
	c.OutboundSheet = a.setting(ctx, "outbound_sheet", a.Cfg.OutboundSheet)
	c.OutboundTab = a.setting(ctx, "outbound_tab", a.Cfg.OutboundTab)
	c.PartnerTab = a.setting(ctx, "partner_tab", a.Cfg.PartnerTab)
	c.ControlTab = a.setting(ctx, "control_tab", a.Cfg.ControlTab)
	return c
}

// OutboundSyncEnabled reports whether outbound sheet writes are allowed.
func (a *App) OutboundSyncEnabled(ctx context.Context) bool {
	if a.Cfg.Mode == "demo" {
		return true
	}
	return a.setting(ctx, "outbound_sync", fmt.Sprint(a.Cfg.OutboundSync)) == "true"
}

func (a *App) clientPath() string { return filepath.Join(a.Cfg.ModeDir(), "google-oauth-client.json") }
func (a *App) tokenPath() string  { return filepath.Join(a.Cfg.ModeDir(), "google-token.json") }

type tokenFile struct {
	RefreshToken string `json:"refresh_token"`
	Account      string `json:"account"`
	CreatedAt    string `json:"created_at"`
}

func (a *App) clientConfig() (google.ClientConfig, string, bool) {
	if a.Cfg.Mode == "demo" {
		return google.ClientConfig{ClientID: fixtures.DemoClientID}, "demo fixture", true
	}
	if b, err := os.ReadFile(a.clientPath()); err == nil {
		if c, err := google.ParseClientJSON(b); err == nil {
			return c, "uploaded client file", true
		}
	}
	if a.Cfg.OAuthClientID != "" {
		return google.ClientConfig{ClientID: a.Cfg.OAuthClientID, ClientSecret: a.Cfg.OAuthClientSecret}, ".env", true
	}
	return google.ClientConfig{}, "", false
}

func (a *App) refreshToken() (string, string, string) {
	if a.Cfg.Mode == "demo" {
		return fixtures.DemoRefresh, fixtures.DemoAccount, "demo fixture"
	}
	if b, err := os.ReadFile(a.tokenPath()); err == nil {
		var t tokenFile
		if json.Unmarshal(b, &t) == nil && t.RefreshToken != "" {
			return t.RefreshToken, t.Account, "connected in app"
		}
	}
	if a.Cfg.OAuthRefreshToken != "" {
		return a.Cfg.OAuthRefreshToken, "", ".env"
	}
	return "", "", ""
}

// Endpoints returns Google endpoints (fixture in demo).
func (a *App) Endpoints() google.Endpoints {
	if a.Fixture != nil {
		return google.Endpoints{Gmail: a.Fixture.URL, Sheets: a.Fixture.URL, Token: a.Fixture.URL + "/token", Auth: a.Fixture.URL + "/o/oauth2/auth"}
	}
	return google.LiveEndpoints
}

// Connect builds authorised Google clients or explains what is missing.
func (a *App) Connect(ctx context.Context) (*jobs.Conn, error) {
	ep := a.Endpoints()
	client, _, hasClient := a.clientConfig()
	refresh, account, _ := a.refreshToken()
	if hasClient && refresh != "" {
		ts := google.RefreshTokenSource(a.hc, ep.Token, client.ClientID, client.ClientSecret, refresh)
		return &jobs.Conn{Gmail: &google.Gmail{Base: ep.Gmail, HTTP: a.hc, Tokens: ts}, Sheets: &google.Sheets{Base: ep.Sheets, HTTP: a.hc, Tokens: ts}, Account: account}, nil
	}
	if a.Cfg.ServiceAccount != "" {
		sa, key, err := google.ParseServiceAccount(a.Cfg.ServiceAccount)
		if err != nil {
			return nil, &netx.ProviderError{Msg: "GOOGLE_SERVICE_ACCOUNT_JSON is invalid: " + err.Error()}
		}
		ts := google.ServiceAccountTokenSource(a.hc, ep.Token, sa, key, google.ScopeSheets)
		return &jobs.Conn{Gmail: &google.Gmail{Base: ep.Gmail, HTTP: a.hc, Tokens: unavailable{}}, Sheets: &google.Sheets{Base: ep.Sheets, HTTP: a.hc, Tokens: ts}}, nil
	}
	if !hasClient {
		return nil, &netx.ProviderError{Msg: "Google is not connected: upload your OAuth client JSON in Settings → Google connection, then click Connect Google."}
	}
	return nil, &netx.ProviderError{Msg: "Google is not connected: click Connect Google in Settings and approve read-only Gmail and Sheets access."}
}

type unavailable struct{}

func (unavailable) Token(context.Context) (string, error) {
	return "", &netx.ProviderError{Msg: "Gmail needs an OAuth connection; a service account can only be used for Sheets."}
}

// Readiness summarises what is configured. Values are booleans or public
// identifiers only; secrets are never returned.
func (a *App) Readiness(ctx context.Context) map[string]any {
	cfg := a.JobConfig(ctx)
	_, clientSource, hasClient := a.clientConfig()
	refresh, account, tokenSource := a.refreshToken()
	google := map[string]any{"client_configured": hasClient, "client_source": clientSource, "connected": hasClient && refresh != "", "token_source": tokenSource, "account": account, "expected_account": cfg.ExpectedAccount, "service_account": a.Cfg.ServiceAccount != ""}
	inbound := map[string]any{"sheet_set": cfg.InboundSheet != "", "sheet_id": cfg.InboundSheet, "tab": cfg.InboundTab, "ready": hasClient && refresh != "" && cfg.InboundSheet != "" && (cfg.ExpectedAccount != "" || a.Cfg.Mode == "demo")}
	outbound := map[string]any{"sheet_set": cfg.OutboundSheet != "", "sheet_id": cfg.OutboundSheet, "tab": cfg.OutboundTab, "partner_tab": cfg.PartnerTab, "control_tab": cfg.ControlTab, "sync_enabled": a.OutboundSyncEnabled(ctx), "ready": cfg.OutboundSheet != "" && ((hasClient && refresh != "") || a.Cfg.ServiceAccount != "") && a.OutboundSyncEnabled(ctx)}
	return map[string]any{
		"mode": a.Cfg.Mode, "version": a.Version, "google": google, "inbound": inbound, "outbound": outbound,
		"discovery":  map[string]any{"provider": "first-party crawler", "paid_search": a.PartnerSearchOn(ctx), "paid_search_requests": a.paidSearches.Load(), "counters": a.Crawler.F.Counters.Snapshot()},
		"extraction": map[string]any{"provider": "deterministic local rules", "hosted_model": false},
		"legacy_adapters": map[string]any{"tavily_key_present": a.Cfg.TavilyKeyPresent, "openrouter_key_present": a.Cfg.OpenRouterKeyPresent, "used_by_go_app": a.PartnerSearchOn(ctx),
			"note": "The Go app never calls OpenRouter. Tavily is used only if you switch on “Search the web for missing partner websites” in Settings (live mode), and only for partner names."},
		"local_model": map[string]any{"enabled": a.Store.GetSetting(ctx, "cfg.llm_enabled", "false") == "true", "model": a.Store.GetSetting(ctx, "cfg.llm_model", ""), "endpoint": a.Cfg.OllamaURL},
		"data_dir":    a.Cfg.ModeDir(), "loopback": a.Cfg.Loopback(), "auto_intake": a.autoIntakeStatus(ctx),
	}
}

// PartnerSearchOn reports whether the opt-in Tavily lookup for missing
// partner websites is enabled: live mode, a key in the environment, and the
// setting switched on.
func (a *App) PartnerSearchOn(ctx context.Context) bool {
	return a.Cfg.Mode == "live" && a.Cfg.TavilyKey != "" && a.Store.GetSetting(ctx, "cfg.partner_search", "false") == "true"
}

// SiteFinder returns the finder for partners without a website. Demo mode
// guesses only demo addresses; live mode tries common fund domains, and adds
// Tavily search only when PartnerSearchOn.
func (a *App) SiteFinder(ctx context.Context) *discovery.SiteFinder {
	f := &discovery.SiteFinder{F: a.Crawler.F}
	if a.Cfg.Mode == "demo" {
		f.TLDs = []string{strings.TrimPrefix(fixtures.DemoSuffix, ".")}
	}
	if a.PartnerSearchOn(ctx) {
		f.Search = discovery.TavilySearch(a.hc, a.searchBase, a.Cfg.TavilyKey, func() { a.paidSearches.Add(1) })
	}
	return f
}
