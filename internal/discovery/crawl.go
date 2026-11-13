package discovery

import (
	"context"
	"encoding/xml"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
	"github.com/itsmepava/nventures-sourcing/internal/extract"
)

// Candidate is a company link discovered on a partner portfolio.
type Candidate struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Partner string `json:"partner"`
	FoundOn string `json:"found_on"`
}

// SourceNote records each source consulted and what happened.
type SourceNote struct {
	URL    string `json:"url"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

// PartnerReport makes discovery breadth and its limits visible.
type PartnerReport struct {
	Partner      string       `json:"partner"`
	Website      string       `json:"website"`
	PagesVisited int          `json:"pages_visited"`
	LinksFound   int          `json:"links_found"`
	Sources      []SourceNote `json:"sources"`
	Candidates   []Candidate  `json:"candidates"`
	Warnings     []string     `json:"warnings"`
	Complete     bool         `json:"complete"`
}

// Limits bound crawling work.
type Limits struct {
	PagesPerPartner   int
	CandidatesPerSite int
	PagesPerCompany   int
}

// DefaultLimits are conservative for daily use.
var DefaultLimits = Limits{PagesPerPartner: 15, CandidatesPerSite: 40, PagesPerCompany: 6}

// Crawler discovers and researches companies.
type Crawler struct {
	F      *Fetcher
	Limits Limits
}

var (
	portfolioRe = regexp.MustCompile(`(?i)portfolio|investments|our-companies|companies|startups|ventures|backed`)
	nextRe      = regexp.MustCompile(`(?i)^(next|older|more|›|»|load more)\b|[?&]page=\d+|/page/\d+`)
	companyPage = regexp.MustCompile(`(?i)about|team|company|story|who-we-are|news|press|blog|funding|investors|contact|careers`)
	feedItemRe  = regexp.MustCompile(`(?i)invest|portfolio|backs|backed|funding|welcome`)
	socialHosts = regexp.MustCompile(`(?i)(^|\.)(linkedin|facebook|twitter|x|instagram|youtube|medium|github|tiktok|t|wa|whatsapp|crunchbase|angel|wellfound|apple|google|play\.google|apps\.apple)\.(com|co|me)$`)
)

// SiteKey approximates the registrable domain of a host.
func SiteKey(host string) string {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	n := 2
	sl := parts[len(parts)-2]
	if len(parts) >= 3 && (sl == "co" || sl == "com" || sl == "org" || sl == "net" || sl == "ac" || sl == "gov" || sl == "edu") && len(parts[len(parts)-1]) == 2 {
		n = 3
	}
	if strings.HasSuffix(host, ".nventures-demo.test") {
		n = 3 // fixture virtual hosts: each subdomain is its own site
	}
	return strings.Join(parts[len(parts)-n:], ".")
}

func sameSite(a, b *url.URL) bool { return SiteKey(a.Hostname()) == SiteKey(b.Hostname()) }

type sitemapDoc struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

type feedDoc struct {
	Items []struct {
		Title string `xml:"title"`
		Link  string `xml:"link"`
	} `xml:"channel>item"`
	Entries []struct {
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func nameFrom(text string, u *url.URL) string {
	text = strings.TrimSpace(text)
	low := strings.ToLower(text)
	if text != "" && len(text) <= 80 && !strings.HasPrefix(low, "http") && low != "visit" && low != "website" && low != "visit website" && low != "learn more" && low != "read more" {
		return text
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	label := strings.Split(host, ".")[0]
	if label == "" {
		return host
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

// DiscoverPartner crawls a partner site for portfolio company links.
func (c *Crawler) DiscoverPartner(ctx context.Context, partner, website string) PartnerReport {
	rep := PartnerReport{Partner: partner, Website: website}
	lim := c.Limits
	if lim.PagesPerPartner == 0 {
		lim = DefaultLimits
	}
	if strings.TrimSpace(website) == "" {
		rep.Warnings = append(rep.Warnings, "No website configured for this partner; add one or research company URLs manually")
		return rep
	}
	if !strings.Contains(website, "://") {
		website = "https://" + website
	}
	home, err := url.Parse(website)
	if err != nil || home.Hostname() == "" {
		rep.Warnings = append(rep.Warnings, "Partner website is not a valid URL")
		return rep
	}
	queue := []string{home.String()}
	seen := map[string]bool{}
	portfolioPages := map[string]bool{}
	if p := strings.Trim(home.Path, "/"); p != "" {
		// A configured deep link (e.g. a partner's portfolio page) is itself
		// the company list, whatever its path is called.
		portfolioPages[home.String()] = true
	}
	for _, sm := range c.F.Sitemaps(ctx, home) {
		r := c.F.Fetch(ctx, sm)
		rep.Sources = append(rep.Sources, SourceNote{sm, r.Status, r.Note})
		rep.PagesVisited++
		var doc sitemapDoc
		if (r.Status == "ok" || r.Status == "cached") && xml.Unmarshal(r.Body, &doc) == nil {
			for _, u := range doc.URLs {
				if portfolioRe.MatchString(u.Loc) {
					queue = append(queue, u.Loc)
					portfolioPages[u.Loc] = true
				}
			}
		}
	}
	found := map[string]bool{}
	fallbackSeen := map[string]bool{}
	var fallback []Candidate
	truncated := false
	for len(queue) > 0 {
		if ctx.Err() != nil {
			rep.Warnings = append(rep.Warnings, "Cancelled before discovery finished")
			return rep
		}
		next := queue[0]
		queue = queue[1:]
		if seen[next] {
			continue
		}
		if rep.PagesVisited >= lim.PagesPerPartner {
			truncated = true
			break
		}
		seen[next] = true
		r := c.F.Fetch(ctx, next)
		rep.PagesVisited++
		rep.Sources = append(rep.Sources, SourceNote{next, r.Status, r.Note})
		if r.Doc == nil {
			if r.Status == "js_only" || r.Status == "blocked" || r.Status == "robots" {
				rep.Warnings = append(rep.Warnings, next+": "+r.Note)
			}
			continue
		}
		if r.Status == "js_only" {
			rep.Warnings = append(rep.Warnings, next+": "+r.Note)
		}
		if r.Status == "truncated" {
			rep.Warnings = append(rep.Warnings, next+": "+r.Note)
		}
		pageURL, _ := url.Parse(r.FinalURL)
		if pageURL == nil {
			pageURL = home
		}
		isPortfolio := portfolioPages[next] || portfolioRe.MatchString(pageURL.Path)
		for _, feed := range r.Doc.Feeds {
			if seen[feed] || rep.PagesVisited >= lim.PagesPerPartner {
				continue
			}
			seen[feed] = true
			fr := c.F.Fetch(ctx, feed)
			rep.PagesVisited++
			rep.Sources = append(rep.Sources, SourceNote{feed, fr.Status, fr.Note})
			var fd feedDoc
			if xml.Unmarshal(fr.Body, &fd) == nil {
				n := 0
				add := func(title, link string) {
					if n < 3 && feedItemRe.MatchString(title) && link != "" {
						queue = append(queue, link)
						portfolioPages[link] = true
						n++
					}
				}
				for _, it := range fd.Items {
					add(it.Title, it.Link)
				}
				for _, it := range fd.Entries {
					add(it.Title, it.Link.Href)
				}
			}
		}
		for _, l := range r.Doc.Links {
			lu, err := url.Parse(l.URL)
			if err != nil || (lu.Scheme != "http" && lu.Scheme != "https") {
				continue
			}
			rep.LinksFound++
			if sameSite(lu, home) {
				if portfolioRe.MatchString(lu.Path+" "+l.Text) && !seen[l.URL] {
					queue = append(queue, l.URL)
					portfolioPages[l.URL] = true
				} else if isPortfolio && nextRe.MatchString(l.Text+" "+lu.RawQuery+" "+lu.Path) && !seen[l.URL] {
					queue = append(queue, l.URL)
					portfolioPages[l.URL] = true
				}
				continue
			}
			if socialHosts.MatchString(lu.Hostname()) || domain.IdentityDomain(lu.String()) == "" {
				continue
			}
			if _, err := c.F.ValidateURL(lu.String()); err != nil {
				rep.Sources = append(rep.Sources, SourceNote{lu.String(), "blocked", "Unsafe link skipped: " + err.Error()})
				continue
			}
			key := SiteKey(lu.Hostname())
			if !isPortfolio {
				// Home-page links are only a fallback when no portfolio page lists companies.
				if next == home.String() && !fallbackSeen[key] {
					fallbackSeen[key] = true
					fallback = append(fallback, Candidate{Name: nameFrom(l.Text, lu), URL: (&url.URL{Scheme: lu.Scheme, Host: lu.Host, Path: "/"}).String(), Partner: partner, FoundOn: next})
				}
				continue
			}
			if found[key] {
				continue
			}
			if len(rep.Candidates) >= lim.CandidatesPerSite {
				truncated = true
				continue
			}
			found[key] = true
			root := &url.URL{Scheme: lu.Scheme, Host: lu.Host, Path: "/"}
			rep.Candidates = append(rep.Candidates, Candidate{Name: nameFrom(l.Text, lu), URL: root.String(), Partner: partner, FoundOn: next})
		}
		for _, ld := range r.Doc.JSONLD {
			if strings.EqualFold(extract.JSONString(ld, "@type"), "ItemList") {
				if items, ok := ld["itemListElement"].([]any); ok {
					for _, it := range items {
						m, _ := it.(map[string]any)
						if m == nil {
							continue
						}
						if inner, ok := m["item"].(map[string]any); ok {
							m = inner
						}
						u, err := url.Parse(extract.JSONString(m, "url"))
						if err != nil || u.Hostname() == "" || sameSite(u, home) || found[SiteKey(u.Hostname())] {
							continue
						}
						found[SiteKey(u.Hostname())] = true
						rep.Candidates = append(rep.Candidates, Candidate{Name: nameFrom(extract.JSONString(m, "name"), u), URL: (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}).String(), Partner: partner, FoundOn: next})
					}
				}
			}
		}
	}
	if truncated || len(queue) > 0 {
		rep.Warnings = append(rep.Warnings, "Crawl limit reached; some portfolio pages or companies were not visited")
	}
	if len(rep.Candidates) == 0 && len(fallback) > 0 {
		if len(fallback) > lim.CandidatesPerSite {
			fallback, truncated = fallback[:lim.CandidatesPerSite], true
		}
		rep.Candidates = fallback
		rep.Warnings = append(rep.Warnings, "No portfolio page found; using external links from the partner home page")
	}
	if len(rep.Candidates) == 0 {
		rep.Warnings = append(rep.Warnings, "No company links found; the portfolio may be JavaScript-only, blocked or unlisted")
	}
	rep.Complete = !truncated && len(queue) == 0 && ctx.Err() == nil
	return rep
}

// ResearchCompany fetches a company's own pages and extracts evidence.
func (c *Crawler) ResearchCompany(ctx context.Context, name, website string) (domain.Record, []SourceNote) {
	lim := c.Limits
	if lim.PagesPerCompany == 0 {
		lim = DefaultLimits
	}
	var notes []SourceNote
	var pages []extract.Page
	home := c.F.Fetch(ctx, website)
	notes = append(notes, SourceNote{website, home.Status, home.Note})
	if home.Doc != nil {
		pages = append(pages, extract.Page{URL: home.FinalURL, Doc: home.Doc, Note: home.Status})
		hu, _ := url.Parse(home.FinalURL)
		var links []string
		seen := map[string]bool{home.FinalURL: true}
		for _, l := range home.Doc.Links {
			lu, err := url.Parse(l.URL)
			if err != nil || hu == nil || !sameSite(lu, hu) || seen[l.URL] {
				continue
			}
			if companyPage.MatchString(lu.Path + " " + l.Text) {
				seen[l.URL] = true
				links = append(links, l.URL)
			}
		}
		sort.SliceStable(links, func(i, j int) bool { return rank(links[i]) < rank(links[j]) })
		for _, l := range links {
			if len(pages) >= lim.PagesPerCompany || ctx.Err() != nil {
				break
			}
			r := c.F.Fetch(ctx, l)
			notes = append(notes, SourceNote{l, r.Status, r.Note})
			if r.Doc != nil {
				pages = append(pages, extract.Page{URL: r.FinalURL, Doc: r.Doc, Note: r.Status})
			}
		}
		if name == "" || strings.EqualFold(name, "visit website") {
			name = titleName(home.Doc.Title)
		}
	}
	rec := extract.Research(name, website, pages)
	for _, n := range notes {
		if n.Status != "ok" && n.Status != "cached" {
			rec.Warnings = append(rec.Warnings, n.URL+": "+n.Status+" "+n.Note)
		}
	}
	return rec, notes
}

func rank(u string) int {
	l := strings.ToLower(u)
	for i, k := range []string{"about", "news", "press", "funding", "company", "team", "story", "blog", "contact"} {
		if strings.Contains(l, k) {
			return i
		}
	}
	return 99
}

func titleName(t string) string {
	for _, sep := range []string{" | ", " - ", " — ", " – ", ": "} {
		if i := strings.Index(t, sep); i > 0 {
			return strings.TrimSpace(t[:i])
		}
	}
	return strings.TrimSpace(t)
}
