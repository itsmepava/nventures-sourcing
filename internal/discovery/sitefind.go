package discovery

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
)

// SiteFinder looks up a partner's website when the partner list has none.
// In order, it tries: the domain of the partner contact's work email; a
// short list of addresses built from the partner's name; and, only when
// configured, an opt-in web search (Tavily). Every candidate is fetched with
// the normal safe fetcher and accepted only if the page itself names the
// partner and is about investing; parked/for-sale pages are rejected.
type SiteFinder struct {
	F *Fetcher
	// TLDs to try for name-based guesses, in order.
	TLDs []string
	// MaxGuesses bounds name-based fetch attempts per partner.
	MaxGuesses int
	// Search is an optional, opt-in web search returning result URLs.
	// nil means no search (the default).
	Search func(ctx context.Context, query string) ([]string, error)
}

// DefaultTLDs covers common fund domains and the South Asia/Singapore region.
var DefaultTLDs = []string{"com", "vc", "capital", "ventures", "co", "fund", "io", "in", "sg", "lk", "pk", "com.bd", "net"}

// SiteResult is the outcome of a lookup.
type SiteResult struct {
	URL    string       `json:"url"`    // "" when nothing trustworthy was found
	Source string       `json:"source"` // email, guess or search
	Note   string       `json:"note"`
	Tried  []SourceNote `json:"tried"`
}

var (
	freeMail     = regexp.MustCompile(`(?i)^(gmail|googlemail|yahoo|ymail|hotmail|outlook|live|msn|icloud|me|mac|aol|proton|protonmail|pm|gmx|mail|yandex|zoho|rediffmail|qq|163)\.`)
	emailHintRe  = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@([a-z0-9\-]+(?:\.[a-z0-9\-]+)+)`)
	investVocab  = regexp.MustCompile(`(?i)\b(invest(s|ing|ment|ments|or|ors)?|portfolio|venture|fund|capital|angels?|startups?|founders?|seed|accelerator|incubator)\b`)
	parkedRe     = regexp.MustCompile(`(?i)domain (is )?(for sale|may be for sale)|buy this domain|this domain is parked|parked free|hugedomains|sedo\.com|dan\.com|afternic|godaddy\.com/domainsearch|domain names? for sale`)
	genericWords = map[string]bool{"the": true, "and": true, "capital": true, "ventures": true, "venture": true, "partners": true, "partner": true, "fund": true, "funds": true,
		"vc": true, "angels": true, "angel": true, "network": true, "group": true, "investments": true, "investment": true, "investors": true, "holdings": true, "management": true,
		"advisors": true, "advisers": true, "llc": true, "llp": true, "ltd": true, "inc": true, "limited": true, "pvt": true, "private": true, "pte": true, "company": true, "co": true,
		"asset": true, "equity": true, "global": true, "demo": true}
	notASite = regexp.MustCompile(`(?i)(^|\.)(linkedin|facebook|twitter|x|instagram|youtube|medium|crunchbase|pitchbook|tracxn|wikipedia|wellfound|angel|dealroom|cbinsights|f6s|techcrunch|bloomberg|reuters|signal\.nfx|github|google|apple|substack)\.[a-z.]+$`)
)

// nameTokens lowercases a partner name into ASCII alphanumeric tokens,
// dropping parenthesised text such as "(demo)".
func nameTokens(name string) []string {
	var b strings.Builder
	depth := 0
	for _, r := range strings.ToLower(name) {
		switch {
		case r == '(':
			depth++
		case r == ')':
			if depth > 0 {
				depth--
			}
		case depth > 0:
		case r == '&':
			b.WriteRune(' ')
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

// coreOf returns the distinctive part of a name ("Indus Bridge Capital" ->
// "indusbridge") and the full compact name.
func coreOf(name string) (core, full string) {
	toks := nameTokens(name)
	var c []string
	for _, t := range toks {
		if !genericWords[t] {
			c = append(c, t)
		}
	}
	full = strings.Join(toks, "")
	core = strings.Join(c, "")
	if core == "" {
		core = full
	}
	return core, full
}

func compact(s string) string {
	return strings.Join(nameTokens(s), "")
}

// Guesses returns candidate home pages for a name, most likely first.
func (s *SiteFinder) Guesses(name string) []string {
	toks := nameTokens(name)
	core, full := coreOf(name)
	if len(full) < 3 {
		return nil
	}
	bases := []string{full}
	add := func(b string) {
		if len(b) >= 3 && len(b) <= 63 {
			for _, x := range bases {
				if x == b {
					return
				}
			}
			bases = append(bases, b)
		}
	}
	add(core)
	add(strings.Join(toks, "-"))
	tlds := s.TLDs
	if len(tlds) == 0 {
		tlds = DefaultTLDs
	}
	var out []string
	for _, b := range bases {
		for _, t := range tlds {
			out = append(out, "https://"+b+"."+t+"/")
		}
	}
	for _, suffix := range []string{"capital", "ventures", "vc", "partners"} {
		if core != full || !strings.HasSuffix(core, suffix) {
			out = append(out, "https://"+core+suffix+"."+tlds[0]+"/")
		}
	}
	// De-duplicate while keeping order.
	seen := map[string]bool{}
	uniq := out[:0]
	for _, u := range out {
		if !seen[u] {
			seen[u] = true
			uniq = append(uniq, u)
		}
	}
	max := s.MaxGuesses
	if max <= 0 {
		max = 30
	}
	if len(uniq) > max {
		uniq = uniq[:max]
	}
	return uniq
}

// Verify decides whether a fetched page is the partner's own site. strict
// requires the partner's name in the page's title, description, structured
// data or first heading; the email-domain hint only needs an investing page.
func Verify(name string, r Result, strict bool) (bool, string) {
	if r.Doc == nil || (r.Status != "ok" && r.Status != "cached" && r.Status != "truncated") {
		return false, "not reachable (" + r.Status + ")"
	}
	text := r.Doc.Title + "\n" + r.Doc.Description + "\n" + r.Doc.Text
	if parkedRe.MatchString(text) {
		return false, "parked or for-sale domain"
	}
	if !investVocab.MatchString(text) {
		return false, "page is not about investing"
	}
	core, full := coreOf(name)
	ids := []string{r.Doc.Title, r.Doc.Description}
	for _, ld := range r.Doc.JSONLD {
		ids = append(ids, extract.JSONString(ld, "name"), extract.JSONString(ld, "alternateName"))
	}
	if i := strings.IndexByte(r.Doc.Text, '\n'); i > 0 && i < 200 {
		ids = append(ids, r.Doc.Text[:i])
	}
	want := core
	if len(core) < 4 {
		want = full // short cores ("ab") are too easy to match by accident
	}
	for _, id := range ids {
		if want != "" && strings.Contains(compact(id), want) {
			return true, "page names the partner"
		}
	}
	if !strict {
		return true, "investing site at the contact's email domain"
	}
	return false, "page does not name the partner"
}

func rootOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}).String()
}

// Find looks up a website for the named partner. contactHint is the
// partner-list contact cell (may contain an email address).
func (s *SiteFinder) Find(ctx context.Context, name, contactHint string) SiteResult {
	var res SiteResult
	try := func(u, source string, strict bool) bool {
		if ctx.Err() != nil {
			return false
		}
		r := s.F.Fetch(ctx, u)
		ok, why := Verify(name, r, strict)
		res.Tried = append(res.Tried, SourceNote{u, r.Status, why})
		if ok {
			final := r.FinalURL
			if final == "" {
				final = u
			}
			res.URL, res.Source, res.Note = rootOf(final), source, why
		}
		return ok
	}
	// 1. Work email domain from the partner list.
	for _, m := range emailHintRe.FindAllStringSubmatch(contactHint, 3) {
		d := strings.ToLower(m[1])
		if freeMail.MatchString(d) {
			continue
		}
		if try("https://"+d+"/", "email", false) {
			res.Note = "contact email domain " + d + "; " + res.Note
			return res
		}
	}
	// 2. Addresses built from the name.
	for _, u := range s.Guesses(name) {
		if try(u, "guess", true) {
			return res
		}
	}
	// 3. Opt-in web search.
	if s.Search != nil && ctx.Err() == nil {
		urls, err := s.Search(ctx, `"`+strings.TrimSpace(name)+`" venture capital investor official website`)
		if err != nil {
			res.Tried = append(res.Tried, SourceNote{"search", "error", netx.SafeMessage(err)})
		}
		seen := map[string]bool{}
		for _, raw := range urls {
			root := rootOf(raw)
			u, _ := url.Parse(root)
			if root == "" || seen[root] || u == nil || notASite.MatchString(u.Hostname()) {
				continue
			}
			seen[root] = true
			if try(root, "search", true) {
				return res
			}
			if len(seen) >= 5 {
				break
			}
		}
	}
	res.Note = "no website found that names this partner; add a Company Portfolio link in the partner list or set it on the Partners page"
	return res
}

// TavilySearch returns a search function for the opt-in Tavily adapter. Only
// the query (a partner name) is sent; the key goes in the Authorization
// header and is never logged. onRequest counts paid requests.
func TavilySearch(hc *http.Client, base, key string, onRequest func()) func(ctx context.Context, q string) ([]string, error) {
	if base == "" {
		base = "https://api.tavily.com"
	}
	return func(ctx context.Context, q string) ([]string, error) {
		if onRequest != nil {
			onRequest()
		}
		var out struct {
			Results []struct {
				URL string `json:"url"`
			} `json:"results"`
		}
		err := netx.DoJSON(ctx, hc, http.MethodPost, strings.TrimRight(base, "/")+"/search", map[string]string{"Authorization": "Bearer " + key},
			map[string]any{"query": q, "max_results": 5, "search_depth": "basic"}, &out, netx.Policy{Attempts: 2})
		if err != nil {
			return nil, err
		}
		urls := make([]string, 0, len(out.Results))
		for _, r := range out.Results {
			urls = append(urls, r.URL)
		}
		return urls, nil
	}
}
