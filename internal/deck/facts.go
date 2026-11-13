package deck

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
	"github.com/itsmepava/nventures-sourcing/internal/extract"
)

// Fact is a reviewable statement found in a deck. Quote is the sentence it
// came from (line breaks joined); Page is 1-based. Facts are suggestions for
// a human reviewer: nothing is applied automatically.
type Fact struct {
	Field string `json:"field"` // description, hq, website, contact, stage, total_funding, raising, founded, founders, traction
	Value string `json:"value"`
	Quote string `json:"quote"`
	Page  int    `json:"page"`
}

var (
	bulletRe    = regexp.MustCompile(`^\s*[•▪◦●○■□➢►▶✓✔\-–*]\s+`)
	hqPhraseRe  = regexp.MustCompile(`(?i:headquartered in|headquarters(?:\s+(?:is|are))?\s*(?:in|:)|\bhq\b(?:\s+is)?\s*(?:in|:)|based (?:in|out of))\s+((?:[\p{Lu}][\p{L}'.\-]*)(?:[ ,]+(?:[\p{Lu}][\p{L}'.\-]*)){0,4})`)
	urlFactRe   = regexp.MustCompile(`(?i)\bhttps?://[^\s<>()"']+|\bwww\.[a-z0-9\-]+(?:\.[a-z0-9\-]+)+[^\s<>()"']*`)
	emailRe     = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9\-]+(?:\.[a-z0-9\-]+)+\b`)
	stageFactRe = regexp.MustCompile(`(?i)\b(pre-?seed|seed|series [a-e]|angel round|angel investors?|bootstrapped|self-funded)\b`)
	fundingRe   = regexp.MustCompile(`(?i)\b(raised|funding|financing|invested|investment)\b`)
	totalRe     = regexp.MustCompile(`(?i)\b(total|in total|to date|so far|cumulative|in all)\b`)
	zeroRe      = regexp.MustCompile(`(?i)\b(no external funding|not raised any|have not raised|bootstrapped|self-funded)\b`)
	raisingRe   = regexp.MustCompile(`(?i)\b(raising|seeking|looking to raise|the ask|our ask|fundraise of)\b`)
	foundedRe   = regexp.MustCompile(`(?i)\b(?:founded|established|started|incorporated) in ((?:19|20)\d\d)\b`)
	foundersRe  = regexp.MustCompile(`(?i:founded (?:in (?:19|20)\d\d )?by|co-?founders?:?|founders?:)\s+([\p{Lu}][\p{L}.'\-]+(?:\s+[\p{Lu}][\p{L}.'\-]+){0,3}(?:\s*(?:,|and|&)\s*[\p{Lu}][\p{L}.'\-]+(?:\s+[\p{Lu}][\p{L}.'\-]+){0,3}){0,4})`)
	tractionRe  = regexp.MustCompile(`(?i)\b\d[\d,.]*\+?\s*(?:k|m)?\s*(?:paying\s+|active\s+)?(?:customers|clients|users|merchants|businesses|pharmacies|clinics|schools|stores|retailers|partners|downloads|subscribers)\b|\b(?:arr|mrr|revenue|gmv)\b`)
	describeRe  = regexp.MustCompile(`(?i)\b(is an?|are an?|builds|we build|helps|we help|provides|offers|platform|software|marketplace|app for|solution for|automation for|analytics for|forecasting for)\b`)
)

// Unwrap joins visually wrapped lines into logical lines: bullets and blank
// lines start a new line; other line breaks become spaces.
func Unwrap(page string) []string {
	var out []string
	cur, joined, bullet := "", false, false
	flush := func() {
		if s := strings.TrimSpace(cur); s != "" {
			out = append(out, s)
		}
		cur, joined, bullet = "", false, false
	}
	for _, line := range strings.Split(page, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			flush()
		case bulletRe.MatchString(t):
			flush()
			cur, bullet = bulletRe.ReplaceAllString(t, ""), true
		case cur == "":
			cur = t
		case strings.HasSuffix(cur, "-") && unicode.IsLower([]rune(t)[0]):
			cur, joined = strings.TrimSuffix(cur, "-")+t, true // hyphenated wrap
		case linkish(lastField(cur)) || linkish(strings.Fields(t)[0]):
			flush() // a line holding a web address or email stands alone
			cur = t
		case !joined && !bullet && heading(cur) && (unicode.IsUpper([]rune(t)[0]) || unicode.IsDigit([]rune(t)[0])):
			flush() // a short title line followed by a new sentence
			cur = t
		default:
			cur, joined = cur+" "+t, true
		}
	}
	flush()
	return out
}

// Facts extracts suggestion facts from deck pages. wrapped is true for PDF
// text, whose line breaks are visual; PPTX lines are already paragraphs.
func Facts(pages []string, wrapped bool) []Fact {
	var facts []Fact
	seen := map[string]bool{}
	add := func(field, value, quote string, page int) {
		value = strings.TrimSpace(strings.Trim(value, " ,;:."))
		if value == "" || utf8.RuneCountInString(value) > 600 {
			return
		}
		k := field + "\x00" + strings.ToLower(value)
		if seen[k] {
			return
		}
		seen[k] = true
		if utf8.RuneCountInString(quote) > 500 {
			quote = string([]rune(quote)[:500]) + "…"
		}
		facts = append(facts, Fact{field, value, quote, page})
	}
	count := map[string]int{}
	limit := map[string]int{"description": 1, "hq": 2, "website": 2, "contact": 3, "stage": 2, "total_funding": 2, "raising": 1, "founded": 1, "founders": 1, "traction": 3}
	want := func(f string) bool { return count[f] < limit[f] }
	put := func(field, value, quote string, page int) {
		if want(field) {
			n := len(facts)
			add(field, value, quote, page)
			if len(facts) > n {
				count[field]++
			}
		}
	}
	for pi, p := range pages {
		page := pi + 1
		lines := strings.Split(p, "\n")
		if wrapped {
			lines = Unwrap(p)
		}
		for _, line := range lines {
			for _, s := range extract.Sentences(line) {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if m := hqPhraseRe.FindStringSubmatch(s); m != nil {
					put("hq", trimLocation(m[1]), s, page)
				}
				for _, u := range urlFactRe.FindAllString(s, -1) {
					u = strings.TrimRight(u, ".,;:!?)")
					if !strings.HasPrefix(strings.ToLower(u), "http") {
						u = "https://" + u
					}
					if domain.IdentityDomain(u) != "" && domain.LinkedInKey(u) == "" {
						put("website", u, s, page)
					}
				}
				for _, e := range emailRe.FindAllString(s, -1) {
					put("contact", strings.ToLower(e), s, page)
				}
				if m := foundedRe.FindStringSubmatch(s); m != nil {
					put("founded", m[1], s, page)
				}
				if m := foundersRe.FindStringSubmatch(s); m != nil {
					put("founders", m[1], s, page)
				}
				amounts := domain.ParseUSDAmounts(s)
				switch {
				case raisingRe.MatchString(s) && len(amounts) > 0:
					put("raising", s, s, page)
				case fundingRe.MatchString(s) && totalRe.MatchString(s) && len(amounts) > 0:
					put("total_funding", s, s, page)
				case zeroRe.MatchString(s):
					put("total_funding", s, s, page)
				}
				if m := stageFactRe.FindString(s); m != "" && (fundingRe.MatchString(s) || raisingRe.MatchString(s) || zeroRe.MatchString(s) || strings.Contains(strings.ToLower(s), "stage") || strings.Contains(strings.ToLower(s), "round")) {
					put("stage", stageValue(m), s, page)
				}
				if tractionRe.MatchString(s) && !fundingRe.MatchString(s) && !raisingRe.MatchString(s) {
					put("traction", s, s, page)
				}
				if page <= 3 && describeRe.MatchString(s) && n(s) >= 30 && n(s) <= 400 && !emailRe.MatchString(s) {
					put("description", s, s, page)
				}
			}
		}
	}
	// A deck's first slide usually has the company name and a tagline; use
	// the tagline if no descriptive sentence was found.
	if want("description") && len(pages) > 0 {
		lines := strings.Split(pages[0], "\n")
		if wrapped {
			lines = Unwrap(pages[0])
		}
		if len(lines) == 1 {
			// Title and tagline may have been joined; nothing reliable to split.
			lines = nil
		}
		for _, l := range lines[min(1, len(lines)):] {
			if n(l) >= 20 && n(l) <= 300 && !emailRe.MatchString(l) && !urlFactRe.MatchString(l) {
				put("description", l, l, 1)
				break
			}
		}
	}
	return facts
}

func lastField(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

func linkish(w string) bool {
	return urlFactRe.MatchString(w) || emailRe.MatchString(w)
}

// heading reports whether a single line looks like a slide title.
func heading(s string) bool {
	if len(strings.Fields(s)) > 5 {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(s)
	return !strings.ContainsRune(",;:-–—", last) && !strings.HasSuffix(strings.ToLower(s), " and") && !strings.HasSuffix(strings.ToLower(s), " for")
}

func stageValue(m string) string {
	m = strings.ToLower(m)
	switch {
	case strings.HasPrefix(m, "angel"):
		return "angel"
	case m == "self-funded":
		return "bootstrapped"
	case strings.HasPrefix(m, "pre"):
		return "pre-seed"
	}
	return m
}

func n(s string) int { return utf8.RuneCountInString(s) }

// trimLocation stops a location phrase at words that start a new clause.
func trimLocation(s string) string {
	f := strings.Fields(s)
	for i, w := range f {
		lw := strings.ToLower(strings.Trim(w, ",."))
		if lw == "and" || lw == "with" || lw == "we" || lw == "since" || lw == "our" || lw == "the" {
			f = f[:i]
			break
		}
	}
	return strings.Trim(strings.Join(f, " "), " ,.")
}
