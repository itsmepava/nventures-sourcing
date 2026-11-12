package discovery

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type rule struct {
	allow bool
	path  string
}

type robots struct {
	rules    []rule
	delay    time.Duration
	sitemaps []string
	denyAll  bool
}

func (r *robots) allowed(path string) bool {
	if r == nil {
		return true
	}
	if r.denyAll {
		return false
	}
	if path == "" {
		path = "/"
	}
	best, allow := -1, true
	for _, x := range r.rules {
		if x.path == "" {
			continue
		}
		if matchRobots(x.path, path) && len(x.path) > best {
			best, allow = len(x.path), x.allow
		} else if matchRobots(x.path, path) && len(x.path) == best && x.allow {
			allow = true
		}
	}
	return allow
}

func matchRobots(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, p := range parts {
		if i == 0 {
			if !strings.HasPrefix(path, p) {
				return false
			}
			pos = len(p)
			continue
		}
		j := strings.Index(path[pos:], p)
		if j < 0 {
			return false
		}
		pos += j + len(p)
	}
	return !anchored || pos == len(path) || (len(parts) > 1 && parts[len(parts)-1] == "")
}

// parseRobots reads groups for our agent, falling back to "*".
func parseRobots(data []byte) *robots {
	type group struct {
		agents []string
		rules  []rule
		delay  time.Duration
	}
	var groups []*group
	var cur *group
	lastWasAgent := false
	out := &robots{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "user-agent":
			if cur == nil || !lastWasAgent {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(v))
			lastWasAgent = true
			continue
		case "allow", "disallow":
			if cur != nil {
				cur.rules = append(cur.rules, rule{allow: k == "allow", path: v})
			}
		case "crawl-delay":
			if cur != nil {
				if s, err := strconv.ParseFloat(v, 64); err == nil {
					cur.delay = min(time.Duration(s*float64(time.Second)), 10*time.Second)
				}
			}
		case "sitemap":
			out.sitemaps = append(out.sitemaps, v)
		}
		lastWasAgent = false
	}
	var star, mine *group
	for _, g := range groups {
		for _, a := range g.agents {
			if a == "*" && star == nil {
				star = g
			}
			if strings.Contains(a, "nventures") {
				mine = g
			}
		}
	}
	if mine == nil {
		mine = star
	}
	if mine != nil {
		out.rules, out.delay = mine.rules, mine.delay
	}
	return out
}

func (f *Fetcher) robotsFor(ctx context.Context, u *url.URL) *robots {
	key := u.Scheme + "://" + u.Host
	f.robotMu.Lock()
	r, ok := f.robots[key]
	f.robotMu.Unlock()
	if ok {
		return r
	}
	ru, _ := url.Parse(key + "/robots.txt")
	r = &robots{}
	if err := f.wait(ctx, u.Host, 0); err == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ru.String(), nil)
		req.Header.Set("User-Agent", UserAgent)
		resp, err := f.client.Do(req)
		switch {
		case err != nil:
			// Unreachable robots: be conservative only for blocked addresses.
			if strings.Contains(err.Error(), ErrBlockedAddress.Error()) {
				r.denyAll = true
			}
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			r.denyAll = true
			resp.Body.Close()
		case resp.StatusCode >= 500:
			r.denyAll = true // temporary server failure: assume disallowed
			resp.Body.Close()
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
			resp.Body.Close()
			r = parseRobots(data)
		default:
			resp.Body.Close()
		}
	}
	f.robotMu.Lock()
	f.robots[key] = r
	f.robotMu.Unlock()
	return r
}

// Sitemaps returns sitemap URLs advertised in robots.txt for a site.
func (f *Fetcher) Sitemaps(ctx context.Context, u *url.URL) []string {
	f.init()
	return f.robotsFor(ctx, u).sitemaps
}
