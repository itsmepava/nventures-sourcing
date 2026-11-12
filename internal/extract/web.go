package extract

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
)

// Page is retrieved text with its URL, as evidence input.
type Page struct {
	URL  string
	Doc  *Document
	Note string
}

var (
	hqCueRe      = regexp.MustCompile(`(?i)\b(headquartered|headquarters|\bhq\b|based in|based out of)\b`)
	b2bRe        = regexp.MustCompile(`(?i)\b(b2b|(?:for|to) (?:businesses|enterprises|companies|teams|smes?|msmes?|small and medium(?:-sized)? (?:businesses|enterprises)|manufacturers|retailers|exporters|clinics|hospitals|banks|lenders|fleets|logistics companies|distributors|merchants|restaurants|schools|utilities|insurers|employers)|enterprise customers|business customers)\b`)
	b2cRe        = regexp.MustCompile(`(?i)\b(b2c|for consumers|consumer app|for individuals|for families|for households|direct[- ]to[- ]consumer)\b`)
	activeRe     = regexp.MustCompile(`(?i)\b(paying customers|customers use|trusted by|serving \d|serves \d|clients include|customers include|in production|live with|used by \d|\d+\+? (customers|clients|businesses|merchants))\b`)
	inactiveRe   = regexp.MustCompile(`(?i)\b(has shut down|ceased operations|no longer operating|we are closing|wound down)\b`)
	stageRe      = regexp.MustCompile(`(?i)\b(pre-?seed|seed|angel|bootstrapped|self-funded|series [a-z]|ipo|acquired by|publicly traded)\b`)
	totalCueRe   = regexp.MustCompile(`(?i)\b(total|cumulative|to date|in all)\b`)
	fundingCueRe = regexp.MustCompile(`(?i)\b(raised|funding|financing|investment|invested|round)\b`)
	zeroCueRe    = regexp.MustCompile(`(?i)no external funding|has not raised|self-funded|bootstrapped`)
	foundedRe    = regexp.MustCompile(`(?i)\b(?:founded|established|started) in ((?:19|20)\d\d)\b`)
	founderRe    = regexp.MustCompile(`(?i)\b(?:founded by|co-?founders? (?:are|is)|founders? (?:are|is))\s+([A-Z][\p{L}.'\- ]{2,80}?)(?:[.,;]| in \d| who )`)
	teamRe       = regexp.MustCompile(`(?i)\b(?:team of|we are) (\d{1,4}) (?:people|employees|engineers|members)\b`)
)

// abbreviations that end with a period but do not end a sentence.
var abbrevs = map[string]bool{"inc": true, "ltd": true, "pvt": true, "co": true, "corp": true, "llc": true, "plc": true, "mr": true, "mrs": true, "ms": true, "dr": true, "st": true, "no": true, "vs": true, "approx": true, "e.g": true, "i.e": true, "u.s": true, "u.k": true, "jan": true, "feb": true, "mar": true, "apr": true, "jun": true, "jul": true, "aug": true, "sep": true, "sept": true, "oct": true, "nov": true, "dec": true, "est": true, "mn": true, "bn": true}

// Sentences splits text into sentences. A period ends a sentence only when
// followed by whitespace and an uppercase letter, digit or quote, and when it
// does not close a known abbreviation or single initial. Decimal numbers such
// as "USD 1.2 million" are therefore never split.
func Sentences(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		r := []rune(line)
		start := 0
		for i := 0; i < len(r); i++ {
			c := r[i]
			if c != '.' && c != '!' && c != '?' {
				continue
			}
			j := i + 1
			for j < len(r) && (r[j] == '.' || r[j] == '!' || r[j] == '?' || r[j] == '"' || r[j] == '\u201d' || r[j] == ')') {
				j++
			}
			if j < len(r) && r[j] != ' ' && r[j] != '\t' {
				continue // e.g. 1.2 or example.com
			}
			k := j
			for k < len(r) && (r[k] == ' ' || r[k] == '\t') {
				k++
			}
			if k < len(r) && !(unicode.IsUpper(r[k]) || unicode.IsDigit(r[k]) || r[k] == '"' || r[k] == '\u201c' || r[k] == '(') {
				continue
			}
			if c == '.' {
				w := i - 1
				for w >= start && (unicode.IsLetter(r[w]) || r[w] == '.') {
					w--
				}
				word := strings.ToLower(string(r[w+1 : i]))
				if abbrevs[word] || (len([]rune(word)) == 1 && unicode.IsLetter(r[i-1])) {
					continue
				}
			}
			out = appendSentence(out, string(r[start:j]))
			start = j
		}
		out = appendSentence(out, string(r[start:]))
	}
	return out
}

func appendSentence(out []string, s string) []string {
	s = strings.TrimSpace(s)
	if len(s) >= 15 && len(s) <= 400 {
		out = append(out, s)
	}
	return out
}

type quote struct{ url, text string }

func addEv(r *domain.Record, field string, q quote) {
	for _, e := range r.Evidence[field] {
		if e.Quote == q.text {
			return
		}
	}
	if len(r.Evidence[field]) < 3 {
		r.Evidence[field] = append(r.Evidence[field], domain.Evidence{URL: q.url, Quote: q.text})
	}
}

// Research extracts an evidence-backed outbound record from first-party
// pages. Every evidenced claim carries an exact sentence from a fetched page.
// Nothing is inferred from the partner, nationality or prior knowledge.
func Research(name, website string, pages []Page) domain.Record {
	r := domain.Record{Name: name, Website: website, Evidence: map[string][]domain.Evidence{}}
	var claims []float64
	hqCountries := map[string]quote{}
	outsideHQ := ""
	stageSeen := ""
	var b2b, b2c, active, inactive bool
	for _, p := range pages {
		if p.Doc == nil {
			continue
		}
		if r.Description == "" && p.Doc.Description != "" {
			r.Description = truncate(p.Doc.Description, 600)
		}
		for _, ld := range p.Doc.JSONLD {
			t := strings.ToLower(JSONString(ld, "@type"))
			if t != "organization" && t != "corporation" && t != "localbusiness" && t != "softwareapplication" {
				continue
			}
			if r.FoundedYear == "" {
				if y := JSONString(ld, "foundingDate"); len(y) >= 4 {
					r.FoundedYear = y[:4]
				}
			}
			for _, s := range JSONStrings(ld, "sameAs") {
				if domain.LinkedInKey(s) != "" && r.LinkedIn == "" {
					r.LinkedIn = s
				}
			}
			if r.Description == "" {
				r.Description = truncate(JSONString(ld, "description"), 600)
			}
		}
		for _, l := range p.Doc.Links {
			if r.LinkedIn == "" && domain.LinkedInKey(l.URL) != "" {
				r.LinkedIn = l.URL
			}
		}
		for _, s := range Sentences(p.Doc.Text) {
			q := quote{p.URL, s}
			if hqCueRe.MatchString(s) {
				matched := false
				for _, c := range domain.Countries {
					if domain.HQExcerptSupports(s, c) {
						if _, ok := hqCountries[c]; !ok {
							hqCountries[c] = q
						}
						matched = true
					}
				}
				if !matched && outsideHQ == "" && regexp.MustCompile(`(?i)headquartered in|headquarters (is |are )?in|\bhq\b[: ]+in`).MatchString(s) {
					outsideHQ = s
					addEv(&r, "headquarters", q)
				}
			}
			if b2bRe.MatchString(s) && !b2cRe.MatchString(s) {
				b2b = true
				addEv(&r, "b2b", q)
			} else if b2cRe.MatchString(s) && !b2bRe.MatchString(s) {
				b2c = true
				addEv(&r, "b2b", q)
			}
			if activeRe.MatchString(s) {
				active = true
				addEv(&r, "active", q)
			}
			if inactiveRe.MatchString(s) {
				inactive = true
				addEv(&r, "active", q)
			}
			if stageRe.MatchString(s) && (fundingCueRe.MatchString(s) || regexp.MustCompile(`(?i)bootstrapped|self-funded|acquired|ipo|publicly traded`).MatchString(s)) {
				st := strings.ToLower(stageRe.FindString(s))
				addEv(&r, "stage", q)
				switch {
				case strings.HasPrefix(st, "series"), st == "ipo", strings.HasPrefix(st, "acquired"), st == "publicly traded":
					stageSeen = "later"
				case stageSeen == "":
					stageSeen = domain.NormalizeStage(strings.ReplaceAll(st, "preseed", "pre-seed"))
					if stageSeen == "self-funded" {
						stageSeen = "bootstrapped"
					}
				}
			}
			amounts := domain.ParseUSDAmounts(s)
			if fundingCueRe.MatchString(s) && len(amounts) > 0 {
				claims = append(claims, amounts...)
				if totalCueRe.MatchString(s) {
					addEv(&r, "funding", q)
					if r.TotalFundingUSD == nil {
						v := amounts[0]
						r.TotalFundingUSD = &v
					}
				} else if r.LastRound == "" {
					r.LastRound = s
					r.Warnings = append(r.Warnings, "Only a single-round amount was found; it is not total funding")
				}
			} else if zeroCueRe.MatchString(s) && r.TotalFundingUSD == nil {
				addEv(&r, "funding", q)
				zero := 0.0
				r.TotalFundingUSD = &zero
			}
			if r.FoundedYear == "" {
				if m := foundedRe.FindStringSubmatch(s); m != nil {
					r.FoundedYear = m[1]
				}
			}
			if r.Founders == "" {
				if m := founderRe.FindStringSubmatch(s); m != nil {
					r.Founders = strings.TrimSpace(m[1])
				}
			}
			if r.TeamSize == "" {
				if m := teamRe.FindStringSubmatch(s); m != nil {
					r.TeamSize = m[1]
				}
			}
		}
		r.Sources = append(r.Sources, domain.Source{URL: p.URL, Title: p.Doc.Title, Status: firstNonEmpty(p.Note, "ok")})
	}
	switch len(hqCountries) {
	case 1:
		for c, q := range hqCountries {
			r.HQCountry, r.Headquarters = c, c
			addEv(&r, "headquarters", q)
		}
	case 0:
		if outsideHQ != "" {
			r.HQCountry = "Outside eligible countries"
			r.Headquarters = outsideHQ
		}
	default:
		r.Warnings = append(r.Warnings, "Sources name more than one HQ country; analyst review required")
		for c, q := range hqCountries {
			addEv(&r, "headquarters", q)
			_ = c
		}
	}
	if outsideHQ != "" && len(hqCountries) > 0 {
		r.Warnings = append(r.Warnings, "Sources also describe a headquarters outside eligible countries")
		r.HQCountry = ""
	}
	switch {
	case b2b && !b2c:
		r.B2B = domain.Bool(true)
		r.Sector = firstNonEmpty(r.Sector, "")
	case b2c && !b2b:
		r.B2B = domain.Bool(false)
	case b2b && b2c:
		r.Warnings = append(r.Warnings, "Both business and consumer positioning found")
	}
	if active && !inactive {
		r.Active = domain.Bool(true)
	} else if inactive {
		r.Active = domain.Bool(false)
	}
	r.Stage = stageSeen
	if stageSeen == "" {
		r.Stage = "unknown"
	}
	r.FundingClaimsUSD = claims
	if r.TotalFundingUSD != nil && len(r.Evidence["funding"]) == 0 {
		r.TotalFundingUSD = nil
	}
	return r
}
