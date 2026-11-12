// Package discovery implements first-party, bounded web discovery: partner
// portfolio crawling and company-page retrieval. It replaces a paid search
// API. It respects robots.txt, rate limits and size limits, refuses private
// network destinations and never circumvents logins or bot protection.
package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
)

// UserAgent identifies the crawler honestly.
const UserAgent = "nVenturesSourcingBot/1.0 (+local first-party research; respects robots.txt)"

// CacheEntry is a cached HTTP response.
type CacheEntry struct {
	URL          string
	Status       int
	ContentType  string
	ETag         string
	LastModified string
	Body         []byte
	Truncated    bool
	FetchedAt    time.Time
}

// Cache stores fetched pages.
type Cache interface {
	GetPage(ctx context.Context, url string) (*CacheEntry, error)
	PutPage(ctx context.Context, e CacheEntry) error
}

// Result describes one fetch.
type Result struct {
	URL         string
	FinalURL    string
	Status      string // ok, cached, blocked, robots, js_only, truncated, mime, error
	HTTPStatus  int
	ContentType string
	Body        []byte
	Doc         *extract.Document
	Note        string
}

// Counters are observable crawl statistics.
type Counters struct {
	Fetched, CacheHits, Blocked, Errors, RobotsDenied atomic.Int64
}

// Snapshot returns counters as a map.
func (c *Counters) Snapshot() map[string]int64 {
	return map[string]int64{"pages_fetched": c.Fetched.Load(), "cache_hits": c.CacheHits.Load(), "blocked": c.Blocked.Load(), "errors": c.Errors.Load(), "robots_denied": c.RobotsDenied.Load()}
}

// Fetcher retrieves web pages safely.
type Fetcher struct {
	Cache       Cache
	MaxBytes    int64
	MinInterval time.Duration
	Freshness   time.Duration
	Timeout     time.Duration
	// Overrides maps a host suffix (e.g. ".nventures-demo.test") to a fixed
	// loopback address. Only the deterministic fixture server uses this; the
	// internet crawler never inherits a loopback exception.
	Overrides map[string]string
	Counters  Counters

	once    sync.Once
	client  *http.Client
	mu      sync.Mutex
	next    map[string]time.Time
	robots  map[string]*robots
	robotMu sync.Mutex
}

// ErrBlockedAddress is returned when a destination resolves to a non-public address.
var ErrBlockedAddress = errors.New("destination address is not a public internet address")

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32", "fc00::/7"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// PublicAddr reports whether ip is a routable public address.
func PublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func (f *Fetcher) override(host string) string {
	host = strings.ToLower(host)
	for suffix, addr := range f.Overrides {
		if strings.HasSuffix(host, suffix) {
			return addr
		}
	}
	return ""
}

func (f *Fetcher) init() {
	f.once.Do(func() {
		if f.MaxBytes == 0 {
			f.MaxBytes = 2 << 20
		}
		if f.Timeout == 0 {
			f.Timeout = 20 * time.Second
		}
		if f.Freshness == 0 {
			f.Freshness = 24 * time.Hour
		}
		f.next = map[string]time.Time{}
		f.robots = map[string]*robots{}
		dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !PublicAddr(ap.Addr()) {
				return ErrBlockedAddress
			}
			return nil
		}}
		fixtureDialer := &net.Dialer{Timeout: 5 * time.Second}
		tr := &http.Transport{
			Proxy: nil, // never route through environment proxies that bypass address checks
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if o := f.override(host); o != "" {
					return fixtureDialer.DialContext(ctx, network, o)
				}
				if port != "80" && port != "443" && port != "8080" && port != "8443" {
					return nil, ErrBlockedAddress
				}
				return dialer.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
		}
		f.client = &http.Client{Transport: fixtureTransport{f, tr}, Timeout: f.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to unsupported scheme")
			}
			return nil // the dialer re-validates every redirect destination
		}}
	})
}

// ValidateURL checks scheme and literal-address destinations before fetching.
func (f *Fetcher) ValidateURL(raw string) (*url.URL, error) {
	f.init()
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("unsupported URL")
	}
	if f.override(u.Hostname()) != "" {
		return u, nil
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !PublicAddr(ip) {
		return nil, ErrBlockedAddress
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || h == "metadata.google.internal" {
		return nil, ErrBlockedAddress
	}
	return u, nil
}

func (f *Fetcher) wait(ctx context.Context, host string, delay time.Duration) error {
	interval := max(f.MinInterval, delay)
	if interval <= 0 {
		return nil
	}
	f.mu.Lock()
	now := time.Now()
	at := f.next[host]
	if at.Before(now) {
		at = now
	}
	f.next[host] = at.Add(interval)
	f.mu.Unlock()
	if d := time.Until(at); d > 0 {
		return netx.SleepCtx(ctx, d)
	}
	return nil
}

var allowedTypes = map[string]bool{"text/html": true, "application/xhtml+xml": true, "text/plain": true, "application/xml": true, "text/xml": true, "application/rss+xml": true, "application/atom+xml": true, "application/json": true, "application/ld+json": true}

// Fetch retrieves a page honouring robots, cache, rate and size limits.
func (f *Fetcher) Fetch(ctx context.Context, raw string) Result {
	f.init()
	res := Result{URL: raw}
	u, err := f.ValidateURL(raw)
	if err != nil {
		f.Counters.Blocked.Add(1)
		res.Status, res.Note = "blocked", "Refused: "+err.Error()
		return res
	}
	rb := f.robotsFor(ctx, u)
	if !rb.allowed(u.EscapedPath()) {
		f.Counters.RobotsDenied.Add(1)
		res.Status, res.Note = "robots", "Disallowed by robots.txt"
		return res
	}
	return f.get(ctx, u, rb.delay)
}

func (f *Fetcher) get(ctx context.Context, u *url.URL, delay time.Duration) Result {
	res := Result{URL: u.String()}
	var cached *CacheEntry
	if f.Cache != nil {
		cached, _ = f.Cache.GetPage(ctx, u.String())
		if cached != nil && time.Since(cached.FetchedAt) < f.Freshness {
			f.Counters.CacheHits.Add(1)
			return f.finish(res, cached, "cached")
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := f.wait(ctx, u.Host, delay); err != nil {
			res.Status, res.Note = "error", "Cancelled"
			return res
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8")
		if cached != nil {
			if cached.ETag != "" {
				req.Header.Set("If-None-Match", cached.ETag)
			}
			if cached.LastModified != "" {
				req.Header.Set("If-Modified-Since", cached.LastModified)
			}
		}
		resp, err := f.client.Do(req)
		if err != nil {
			if errors.Is(err, ErrBlockedAddress) {
				f.Counters.Blocked.Add(1)
				res.Status, res.Note = "blocked", "Refused: destination resolves to a private or reserved address"
				return res
			}
			if ctx.Err() != nil {
				res.Status, res.Note = "error", "Cancelled"
				return res
			}
			f.Counters.Errors.Add(1)
			res.Status, res.Note = "error", "Network error"
			return res
		}
		f.Counters.Fetched.Add(1)
		res.HTTPStatus = resp.StatusCode
		if resp.Request != nil && resp.Request.URL != nil {
			res.FinalURL = resp.Request.URL.String()
		}
		if resp.StatusCode == http.StatusNotModified && cached != nil {
			resp.Body.Close()
			cached.FetchedAt = time.Now()
			if f.Cache != nil {
				_ = f.Cache.PutPage(ctx, *cached)
			}
			f.Counters.CacheHits.Add(1)
			return f.finish(res, cached, "cached")
		}
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			wait := netx.RetryAfter(resp.Header.Get("Retry-After"), 10*time.Second)
			resp.Body.Close()
			if attempt < 2 {
				if wait == 0 {
					wait = time.Duration(1<<attempt) * time.Second
				}
				if err := netx.SleepCtx(ctx, wait); err != nil {
					res.Status, res.Note = "error", "Cancelled"
					return res
				}
				continue
			}
			f.Counters.Errors.Add(1)
			res.Status, res.Note = "error", fmt.Sprintf("Rate limited (HTTP %d) after bounded retries", resp.StatusCode)
			return res
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			resp.Body.Close()
			f.Counters.Blocked.Add(1)
			res.Status, res.Note = "blocked", fmt.Sprintf("Access restricted (HTTP %d); not bypassed", resp.StatusCode)
			return res
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			resp.Body.Close()
			f.Counters.Errors.Add(1)
			res.Status, res.Note = "error", fmt.Sprintf("HTTP %d", resp.StatusCode)
			return res
		}
		ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if ct == "" {
			ct = "text/html"
		}
		if !allowedTypes[ct] {
			resp.Body.Close()
			res.Status, res.Note, res.ContentType = "mime", "Skipped unsupported content type "+ct, ct
			return res
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, f.MaxBytes+1))
		resp.Body.Close()
		if rerr != nil {
			f.Counters.Errors.Add(1)
			res.Status, res.Note = "error", "Response interrupted"
			return res
		}
		e := CacheEntry{URL: u.String(), Status: resp.StatusCode, ContentType: ct, ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"), Body: body, FetchedAt: time.Now()}
		if int64(len(body)) > f.MaxBytes {
			e.Body, e.Truncated = body[:f.MaxBytes], true
		}
		if f.Cache != nil {
			_ = f.Cache.PutPage(ctx, e)
		}
		return f.finish(res, &e, "ok")
	}
	res.Status = "error"
	return res
}

func (f *Fetcher) finish(res Result, e *CacheEntry, status string) Result {
	res.Status, res.Body, res.ContentType = status, e.Body, e.ContentType
	if res.FinalURL == "" {
		res.FinalURL = e.URL
	}
	if e.Truncated {
		res.Note = fmt.Sprintf("Truncated at %d bytes", len(e.Body))
	}
	if strings.Contains(e.ContentType, "html") {
		base, _ := url.Parse(res.FinalURL)
		if doc, err := extract.ParseHTML(bytes.NewReader(e.Body), base); err == nil {
			res.Doc = doc
			if len(doc.Text) < 200 && (doc.Scripts > 0 || doc.NoScriptMsg) {
				res.Status, res.Note = "js_only", "Page content requires JavaScript; add company URLs manually"
			}
		}
	} else if e.ContentType == "text/plain" {
		res.Doc = &extract.Document{Text: string(e.Body)}
	}
	if e.Truncated && res.Status != "js_only" {
		res.Status = "truncated"
	}
	return res
}

// fixtureTransport serves https URLs of overridden fixture hosts over the
// loopback fixture's plain HTTP listener, keeping the original URL visible.
type fixtureTransport struct {
	f    *Fetcher
	next http.RoundTripper
}

func (t fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" && t.f.override(req.URL.Hostname()) != "" {
		orig := req
		req = req.Clone(req.Context())
		req.URL.Scheme = "http"
		resp, err := t.next.RoundTrip(req)
		if resp != nil {
			resp.Request = orig
		}
		return resp, err
	}
	return t.next.RoundTrip(req)
}
