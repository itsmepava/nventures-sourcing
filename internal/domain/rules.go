// Package domain holds the deterministic investment rules. Nothing in here
// performs I/O, and no model or provider output can override a decision.
package domain

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Countries is the closed list of eligible actual-HQ countries for outbound sourcing.
var Countries = []string{"Afghanistan", "Bangladesh", "Bhutan", "India", "Maldives", "Nepal", "Pakistan", "Sri Lanka", "Singapore"}

// FundingLimitUSD is the inclusive ceiling on evidenced total historical funding.
const FundingLimitUSD = 3_000_000

var earlyStages = map[string]bool{"bootstrapped": true, "pre-seed": true, "seed": true, "angel": true}

// Evidence is an exact excerpt from a retrieved source.
type Evidence struct {
	URL   string `json:"url"`
	Quote string `json:"quote"`
}

// Record is the outbound research record. Pointer fields distinguish
// "unknown" from a false/zero value: unknown never qualifies.
type Record struct {
	Name             string                `json:"name"`
	Headquarters     string                `json:"headquarters"`
	HQCountry        string                `json:"hq_country"`
	Sector           string                `json:"sector"`
	Stage            string                `json:"stage"`
	B2B              *bool                 `json:"b2b"`
	Active           *bool                 `json:"active"`
	TotalFundingUSD  *float64              `json:"total_funding_usd"`
	FundingClaimsUSD []float64             `json:"funding_claims_usd"`
	Website          string                `json:"website"`
	LinkedIn         string                `json:"linkedin"`
	Description      string                `json:"description"`
	Product          string                `json:"product"`
	TargetMarket     string                `json:"target_market"`
	Founders         string                `json:"founders"`
	FounderLinkedIn  string                `json:"founder_linkedin"`
	FoundedYear      string                `json:"founded_year"`
	TeamSize         string                `json:"team_size"`
	Customers        string                `json:"customers"`
	CustomerCount    string                `json:"customer_count"`
	Revenue          string                `json:"revenue"`
	UVP              string                `json:"uvp"`
	LastRound        string                `json:"last_round"`
	Investors        string                `json:"investors"`
	Competitors      string                `json:"competitors"`
	Vision           string                `json:"vision"`
	Evidence         map[string][]Evidence `json:"evidence"`
	Sources          []Source              `json:"sources,omitempty"`
	Warnings         []string              `json:"warnings,omitempty"`
	Demo             bool                  `json:"demo,omitempty"`
}

// Source records a fetched page used for evidence.
type Source struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	FetchedAt string `json:"fetched_at,omitempty"`
	Status    string `json:"status,omitempty"` // ok, cached, blocked, js_only, truncated, error
	Content   string `json:"content,omitempty"`
	Note      string `json:"note,omitempty"`
}

func Bool(v bool) *bool        { return &v }
func Float(v float64) *float64 { return &v }

var (
	hqClauseRe   = regexp.MustCompile(`(?i)(?:headquartered|headquarters|\bhq|\bbased)\b([^.;\n]{0,90})`)
	hqStopRe     = regexp.MustCompile(`(?i)\b(?:founder|founders|market|markets|operations|office|offices|serving|serves|customers|but|while)\b`)
	nameSuffixRe = regexp.MustCompile(`\b(private|pvt|limited|ltd|llp|inc|incorporated|corp|corporation)\b`)
	amountRe     = regexp.MustCompile(`(?i)(?:USD|US\$|\$)\s*([\d,]+(?:\.\d+)?)\s*(billion|million|bn|[mbk])?\b`)
	zeroRe       = regexp.MustCompile(`(?i)no external funding|has not raised|self.funded|bootstrapped`)
	usdRe        = regexp.MustCompile(`(?i)\bUSD\b|US\$|U\.?S\.?\s+dollars`)
	totalRe      = regexp.MustCompile(`(?i)\btotal\b|\bcumulative\b|\bto date\b|\bin all\b`)
	maturityRe   = regexp.MustCompile(`(?i)\bseries\s+[a-z]\b|\bipo\b|\bacquired\b|\bpublicly traded\b`)
)

// HQExcerptSupports reports whether a quote states the HQ is in country,
// ignoring founder nationality, markets and satellite offices.
func HQExcerptSupports(quote, country string) bool {
	countryRe := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(country) + `\b`)
	for _, m := range hqClauseRe.FindAllStringSubmatch(quote, -1) {
		clause := m[1]
		if loc := hqStopRe.FindStringIndex(clause); loc != nil {
			clause = clause[:loc[0]]
		}
		if countryRe.MatchString(clause) {
			return true
		}
	}
	return false
}

// NameKey normalizes a company name for identity comparison.
func NameKey(v string) string {
	v = nameSuffixRe.ReplaceAllString(strings.ToLower(v), "")
	var b strings.Builder
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseLoose(raw string) *url.URL {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return u
}

// DomainKey returns the lowercase host without www., or "".
func DomainKey(raw string) string {
	u := parseLoose(raw)
	if u == nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// LinkedInKey returns a canonical company LinkedIn path, or "".
func LinkedInKey(raw string) string {
	u := parseLoose(raw)
	if u == nil || strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") != "linkedin.com" || !strings.HasPrefix(u.Path, "/company/") {
		return ""
	}
	return "linkedin.com" + strings.TrimRight(strings.ToLower(u.Path), "/")
}

// SharedHosts never identify a single business: social networks, document
// hosts and mail providers are shared by unrelated companies.
var SharedHosts = map[string]bool{
	"linkedin.com": true, "drive.google.com": true, "docs.google.com": true, "facebook.com": true,
	"instagram.com": true, "docsend.com": true, "x.com": true, "twitter.com": true, "youtube.com": true,
	"dropbox.com": true, "notion.so": true, "notion.site": true, "pitch.com": true, "canva.com": true,
	"medium.com": true, "github.com": true, "linktr.ee": true, "wixsite.com": true, "gmail.com": true,
	"google.com": true, "sites.google.com": true, "bit.ly": true, "calendly.com": true, "angel.co": true,
	"wellfound.com": true, "crunchbase.com": true, "tracxn.com": true, "f6s.com": true,
}

// IdentityDomain returns a domain usable for identity, excluding shared hosts.
func IdentityDomain(raw string) string {
	d := DomainKey(raw)
	if d == "" || SharedHosts[d] {
		return ""
	}
	for h := range SharedHosts {
		if strings.HasSuffix(d, "."+h) {
			return ""
		}
	}
	return d
}

// Identity is the set of keys used for duplicate detection.
type Identity struct {
	NameKey     string `json:"name_key"`
	Domain      string `json:"domain"`
	LinkedInKey string `json:"linkedin_key"`
}

func IdentityOf(name, website, linkedin string) Identity {
	return Identity{NameKey: NameKey(name), Domain: IdentityDomain(website), LinkedInKey: LinkedInKey(linkedin)}
}

// Matches reports whether two identities refer to the same company.
func (a Identity) Matches(b Identity) bool {
	return (a.NameKey != "" && a.NameKey == b.NameKey) || (a.Domain != "" && a.Domain == b.Domain) || (a.LinkedInKey != "" && a.LinkedInKey == b.LinkedInKey)
}

// Duplicate reports whether id matches any known identity.
func Duplicate(id Identity, known []Identity) bool {
	for _, k := range known {
		if id.Matches(k) {
			return true
		}
	}
	return false
}

// ParseUSDAmounts returns every explicit USD amount in text.
func ParseUSDAmounts(text string) []float64 {
	var out []float64
	for _, m := range amountRe.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(m[2]) {
		case "billion", "bn", "b":
			v *= 1e9
		case "million", "m":
			v *= 1e6
		case "k":
			v *= 1e3
		}
		out = append(out, v)
	}
	return out
}

func supported(refs []Evidence) bool {
	for _, x := range refs {
		if (strings.HasPrefix(x.URL, "https://") || strings.HasPrefix(x.URL, "http://")) && len(strings.TrimSpace(x.Quote)) >= 15 {
			return true
		}
	}
	return false
}

func joinQuotes(refs []Evidence) string {
	parts := make([]string, 0, len(refs))
	for _, x := range refs {
		parts = append(parts, x.Quote)
	}
	return strings.Join(parts, " ")
}

// NormalizeStage maps free text to the canonical stage vocabulary.
func NormalizeStage(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "pre seed", "pre-seed")
	s = strings.ReplaceAll(s, "preseed", "pre-seed")
	return s
}

// Qualifies returns the reasons a record fails the outbound mandate.
// An empty slice means every hard requirement is evidenced. Absent or
// ambiguous evidence fails closed.
func Qualifies(r Record) []string {
	var reasons []string
	ev := r.Evidence
	if ev == nil {
		ev = map[string][]Evidence{}
	}
	country := strings.TrimSpace(r.HQCountry)
	eligible := false
	for _, c := range Countries {
		if c == country {
			eligible = true
		}
	}
	if !eligible && country != "" && !strings.EqualFold(country, "unknown") && supported(ev["headquarters"]) {
		reasons = append(reasons, "Headquarters evidenced outside eligible countries")
	}
	if !eligible || strings.TrimSpace(r.Headquarters) == "" || !supported(ev["headquarters"]) {
		reasons = append(reasons, "Eligible headquarters not established with evidence")
	} else {
		ok := false
		for _, x := range ev["headquarters"] {
			if HQExcerptSupports(x.Quote, country) {
				ok = true
			}
		}
		if !ok {
			reasons = append(reasons, "HQ evidence does not explicitly establish the claimed country")
		}
	}
	if r.B2B != nil && !*r.B2B && supported(ev["b2b"]) {
		reasons = append(reasons, "Consumer (non-B2B) business model evidenced")
	}
	if r.B2B == nil || !*r.B2B || !supported(ev["b2b"]) {
		reasons = append(reasons, "B2B business model not verified")
	}
	if r.Active == nil || !*r.Active || !supported(ev["active"]) {
		reasons = append(reasons, "Active operating company not verified")
	}
	if !earlyStages[NormalizeStage(r.Stage)] || !supported(ev["stage"]) {
		reasons = append(reasons, "Early stage not verified (bootstrapped / angel / pre-seed / seed only)")
	}
	amount := r.TotalFundingUSD
	if amount == nil || math.IsNaN(*amount) || math.IsInf(*amount, 0) || *amount < 0 || !supported(ev["funding"]) {
		reasons = append(reasons, "Total funding in USD not verified")
	} else if *amount > FundingLimitUSD {
		reasons = append(reasons, "Total funding exceeds $3,000,000")
	}
	fundingText := joinQuotes(ev["funding"])
	excerpt := ParseUSDAmounts(fundingText)
	for _, v := range excerpt {
		if v > FundingLimitUSD {
			reasons = append(reasons, "Independent funding excerpt exceeds ceiling")
		}
	}
	zeroBootstrap := amount != nil && *amount == 0 && zeroRe.MatchString(fundingText)
	if !zeroBootstrap {
		inExcerpt := false
		for _, v := range excerpt {
			if amount != nil && v == *amount {
				inExcerpt = true
			}
		}
		if !usdRe.MatchString(fundingText) || !totalRe.MatchString(fundingText) || !inExcerpt {
			reasons = append(reasons, "Funding excerpt must explicitly support the total USD amount")
		}
	}
	for _, v := range r.FundingClaimsUSD {
		if v > FundingLimitUSD {
			reasons = append(reasons, "Conflicting evidence exceeds funding ceiling")
			break
		}
	}
	if maturityRe.MatchString(joinQuotes(ev["stage"])) {
		reasons = append(reasons, "Series A+ or mature-company evidence")
	}
	if strings.TrimSpace(r.Name) == "" || NameKey(r.Name) == "" {
		reasons = append(reasons, "Company name missing")
	}
	return reasons
}

// Contradiction reports whether the reasons include evidence against the
// mandate (as opposed to merely missing evidence). Contradicted candidates
// are rejected; missing-evidence candidates go to the research queue.
func Contradiction(reasons []string) bool {
	for _, r := range reasons {
		switch r {
		case "Total funding exceeds $3,000,000", "Independent funding excerpt exceeds ceiling",
			"Conflicting evidence exceeds funding ceiling", "Series A+ or mature-company evidence",
			"Company name missing", "Headquarters evidenced outside eligible countries",
			"Consumer (non-B2B) business model evidenced":
			return true
		}
	}
	return false
}
