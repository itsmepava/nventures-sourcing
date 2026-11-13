package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
)

// InboundHeaders is the required six-column inbound layout.
var InboundHeaders = []string{"Company Name", "Description", "HQ", "Contact", "Status", "Notes"}

// Norm normalizes a header for comparison.
func Norm(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// ValidateInbound checks the six expected headers. No cells are changed.
func ValidateInbound(rows [][]string) error {
	if len(rows) == 0 {
		return &netx.ProviderError{Msg: "Inbound sheet is empty; add the header row Company Name, Description, HQ, Contact, Status, Notes. No cells were changed."}
	}
	h := rows[0]
	if len(h) < 6 {
		return &netx.ProviderError{Msg: "Inbound sheet must have the six expected headers. No cells were changed."}
	}
	for i, want := range InboundHeaders {
		if Norm(h[i]) != Norm(want) {
			return &netx.ProviderError{Msg: fmt.Sprintf("Inbound sheet column %d is %q; expected %q. No cells were changed.", i+1, h[i], want)}
		}
	}
	return nil
}

// InboundRow builds the six-column row. Status is always blank: the
// investment Status column belongs to the team and is never written.
func InboundRow(name, description, hq, contact, notes string) []string {
	return []string{name, description, hq, contact, "", notes}
}

// OutboundFields maps legacy 38-column headers (typo aliases included) to record fields.
var OutboundFields = map[string]string{"Company Name": "name", "Country": "hq_country", "Sector": "sector", "Business Model": "business_model", "Stage": "stage", "Founded year": "founded_year", "Headquarters": "headquarters", "Website": "website", "Company LinkedIn": "linkedin", "Company Description": "description", "Product Descriptoon": "product", "Product Description": "product", "Target Market": "target_market", "Team Size": "team_size", "Number of Customers": "customer_count", "Notable Customers": "customers", "Revenue Generated": "revenue", "Unique Value Propsotions": "uvp", "Unique Value Propositions": "uvp", "Competitors": "competitors", "Vision": "vision", "Founder Name": "founders", "Founder LinkedIn": "founder_linkedin", "Last Round": "last_round", "Notable Investors": "investors", "Total Funding USD": "total_funding_usd"}

var outboundMap = func() map[string]string {
	m := map[string]string{}
	for k, v := range OutboundFields {
		m[Norm(k)] = v
	}
	return m
}()

// ValidateOutbound requires a Company Name header.
func ValidateOutbound(rows [][]string) error {
	if len(rows) == 0 {
		return &netx.ProviderError{Msg: "Outbound sheet is empty; it needs a header row with Company Name. No cells were changed."}
	}
	for _, h := range rows[0] {
		if Norm(h) == "company name" {
			return nil
		}
	}
	return &netx.ProviderError{Msg: "Outbound sheet requires a Company Name header. No cells were changed."}
}

func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// OutboundRow maps a record onto whatever headers the sheet has. Unknown
// headers stay blank; duplicate Extra Notes headers both receive notes.
func OutboundRow(headers []string, record json.RawMessage, partner string) []string {
	var values map[string]any
	_ = json.Unmarshal(record, &values)
	if values == nil {
		values = map[string]any{}
	}
	values["business_model"] = "B2B"
	if s, _ := values["last_round"].(string); s == "" || len(s) > 120 {
		values["last_round"] = values["stage"]
	}
	notes := "Sourced via " + partner + ". First-party evidence extracted by nVentures; analyst review required."
	hasFunding := false
	for _, h := range headers {
		if outboundMap[Norm(h)] == "total_funding_usd" {
			hasFunding = true
		}
	}
	if f, ok := values["total_funding_usd"].(float64); ok && !hasFunding {
		// The sheet has no funding column: keep the evidenced total visible.
		notes += " Total funding to date (evidenced): USD " + usd(f) + "."
	}
	defaults := map[string]string{"outreach status": "Not Reached Out", "status": "Sourced", "ic recommendation": "Potential Fit",
		"reason":                   "B2B; verified eligible HQ; early stage; total funding <= $3M",
		"extra notes":              notes,
		"updates":                  "Sourced via nVentures Sourcing (Go)",
		"updates (sourcing sheet)": "Sourced via nVentures Sourcing (Go)",
		"source":                   "nVentures Sourcing via " + partner}
	row := make([]string, len(headers))
	for i, h := range headers {
		n := Norm(h)
		if f, ok := outboundMap[n]; ok {
			row[i] = cellString(values[f])
		} else {
			row[i] = defaults[n]
		}
	}
	return row
}

// usd formats a whole-dollar amount with thousands separators.
func usd(f float64) string {
	s := strconv.FormatInt(int64(math.Round(f)), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func pad(r []string, n int) []string {
	out := make([]string, n)
	copy(out, r)
	return out
}

func equalRow(existing, want []string) bool {
	n := max(len(existing), len(want))
	a, b := pad(existing, n), pad(want, n)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func colName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

var updatedRe = regexp.MustCompile(`!\$?[A-Z]+\$?(\d+)`)

// Matcher decides whether an existing row is the same company.
type Matcher func(headers, row []string) bool

// InboundMatcher matches by company name key or a non-shared website host.
func InboundMatcher(nameKey, website string) Matcher {
	host := domain.IdentityDomain(website)
	var hostRe *regexp.Regexp
	if host != "" {
		hostRe = regexp.MustCompile(`(?:^|[^\w.-])(?:www\.)?` + regexp.QuoteMeta(host) + `(?:$|[^\w.-])`)
	}
	return func(_, row []string) bool {
		if len(row) > 0 && nameKey != "" && domain.NameKey(row[0]) == nameKey {
			return true
		}
		return hostRe != nil && hostRe.MatchString(strings.ToLower(strings.Join(row, " ")))
	}
}

// OutboundMatcher matches by name, website domain or company LinkedIn.
func OutboundMatcher(id domain.Identity) Matcher {
	return func(headers, row []string) bool {
		get := func(h string) string {
			for i, x := range headers {
				if Norm(x) == h && i < len(row) {
					return row[i]
				}
			}
			return ""
		}
		return id.Matches(domain.IdentityOf(get("company name"), get("website"), get("company linkedin")))
	}
}

// Result is the outcome of one row sync.
type Result struct {
	Status string // verified, already_exists, conflict, uncertain, failed
	Row    int64
	Range  string
	Err    error
}

// ErrNotVerified marks a write that could not be confirmed.
var ErrNotVerified = errors.New("not verified")

// SyncRow reconciles then appends one row and verifies it by read-back.
//
//  1. If an identical row exists (for example after a lost response) the
//     earlier write is adopted as verified without appending again.
//  2. If the same company exists with different contents, nothing is written:
//     already_exists normally, conflict if a previous write was uncertain.
//  3. Otherwise beforeWrite persists the intent, the row is appended once with
//     RAW input, the returned row is read back in full and compared.
func SyncRow(ctx context.Context, s *Sheets, id, tab string, snapshot [][]string, row []string, match Matcher, priorUncertain bool, beforeWrite func() error) Result {
	headers := snapshot[0]
	for i, existing := range snapshot[1:] {
		if equalRow(existing, row) {
			n := int64(i + 2)
			return Result{Status: "verified", Row: n, Range: fmt.Sprintf("%s!A%d:%s%d", QuoteTab(tab), n, colName(len(row)), n)}
		}
	}
	for i, existing := range snapshot[1:] {
		if match(headers, existing) {
			n := int64(i + 2)
			if priorUncertain {
				return Result{Status: "conflict", Row: n, Err: &netx.ProviderError{Msg: fmt.Sprintf("Row %d matches this company but differs from the uncertain earlier write; inspect the sheet. Nothing was written.", n)}}
			}
			return Result{Status: "already_exists", Row: n}
		}
	}
	if err := beforeWrite(); err != nil {
		return Result{Status: "failed", Err: err}
	}
	rng, err := s.Append(ctx, id, tab, row)
	if err != nil {
		if netx.IsUncertain(err) {
			return Result{Status: "uncertain", Err: &netx.ProviderError{Msg: "Write outcome uncertain (" + netx.SafeMessage(err) + "). The next sync reads the sheet and reconciles before appending.", Uncertain: true}}
		}
		return Result{Status: "failed", Err: err}
	}
	m := updatedRe.FindStringSubmatch(rng)
	if m == nil {
		return Result{Status: "uncertain", Err: &netx.ProviderError{Msg: "Sheets did not return the written range; the next sync will reconcile before appending.", Uncertain: true}}
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	readRange := fmt.Sprintf("%s!A%d:%s%d", QuoteTab(tab), n, colName(max(len(row), 1)), n)
	got, err := s.Values(ctx, id, readRange)
	if err != nil {
		return Result{Status: "uncertain", Row: n, Range: rng, Err: &netx.ProviderError{Msg: "Row written but read-back failed; the next sync will verify it.", Uncertain: true}}
	}
	var actual []string
	if len(got) > 0 {
		actual = got[0]
	}
	if !equalRow(actual, row) {
		return Result{Status: "conflict", Row: n, Range: rng, Err: &netx.ProviderError{Msg: fmt.Sprintf("Read-back of row %d did not match what was written; inspect the sheet before retrying.", n)}}
	}
	return Result{Status: "verified", Row: n, Range: rng}
}

// FullRange is the A1 range used to snapshot a tab.
func FullRange(tab string) string { return QuoteTab(tab) + "!A:AZ" }
