package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
)

func TestOutbound38ColumnAliasMapping(t *testing.T) {
	headers := []string{"Company Name", "Product Descriptoon", "Unique Value Propsotions", "Founder Name ", "Extra Notes", "Extra Notes", "Total Funding USD", "Status", "Business Model"}
	for len(headers) < 38 {
		headers = append(headers, "Unknown Column")
	}
	rec, _ := json.Marshal(map[string]any{"name": "Acme", "product": "Widget", "uvp": "Fast", "founders": "A. Founder", "total_funding_usd": 800000.0, "stage": "seed"})
	row := OutboundRow(headers, rec, "Partner X")
	if len(row) != 38 || row[0] != "Acme" || row[1] != "Widget" || row[2] != "Fast" || row[3] != "A. Founder" {
		t.Fatalf("%q", row[:6])
	}
	if row[4] == "" || row[4] != row[5] || !strings.Contains(row[4], "Partner X") {
		t.Error("duplicate Extra Notes not both filled")
	}
	if row[6] != "800000" || row[7] != "Sourced" || row[8] != "B2B" || row[37] != "" {
		t.Errorf("values %q %q %q %q", row[6], row[7], row[8], row[37])
	}
}

func TestInboundHeaderValidation(t *testing.T) {
	if err := ValidateInbound([][]string{{"Company Name", "Description", "HQ", "Contact", "Status", "Notes"}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInbound([][]string{{" company  name", "description", "HQ", "Contact", "Status", "Notes", "Extra"}}); err != nil {
		t.Error("whitespace/case/extra columns should be tolerated")
	}
	for _, bad := range [][][]string{{}, {{"Company", "Description"}}, {{"Company Name", "Desc", "HQ", "Contact", "Status", "Notes"}}} {
		if ValidateInbound(bad) == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if InboundRow("n", "d", "", "c", "notes")[4] != "" {
		t.Error("status must be blank")
	}
}

func TestMatchersNeverMergeOnSharedHosts(t *testing.T) {
	m := InboundMatcher(domain.NameKey("Alpha"), "https://docs.google.com/presentation/d/abc")
	if m(nil, []string{"Beta", "Deck https://docs.google.com/presentation/d/xyz", "", "same@founder.example", "", ""}) {
		t.Error("shared document host merged businesses")
	}
	m = InboundMatcher(domain.NameKey("Alpha"), "https://alpha.example")
	if !m(nil, []string{"Alpha Labs Pvt Ltd", ""}) && !m(nil, []string{"Other", "see www.alpha.example"}) {
		t.Error("website match missed")
	}
	if m(nil, []string{"Other", "see notalpha.example"}) {
		t.Error("substring host matched")
	}
	om := OutboundMatcher(domain.IdentityOf("Acme", "https://acme.example", ""))
	if !om([]string{"Company Name", "Website"}, []string{"Different", "https://www.acme.example/about"}) {
		t.Error("outbound website match missed")
	}
}

func TestServiceAccountJWTAndClientJSON(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	raw, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "svc@example.iam", "private_key": pemKey})
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm.Get("assertion")
		w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	}))
	defer srv.Close()
	for _, enc := range []string{string(raw), base64.StdEncoding.EncodeToString(raw)} {
		sa, k, err := ParseServiceAccount(enc)
		if err != nil {
			t.Fatal(err)
		}
		tok, err := ServiceAccountTokenSource(srv.Client(), srv.URL, sa, k, ScopeSheets).Token(context.Background())
		if err != nil || tok != "tok" || strings.Count(got, ".") != 2 {
			t.Fatalf("%v %q", err, got)
		}
	}
	if _, err := ParseClientJSON([]byte(`{"installed":{"client_id":"x.apps.googleusercontent.com","client_secret":"s"}}`)); err != nil {
		t.Error(err)
	}
	if _, err := ParseClientJSON([]byte(`{"other":{}}`)); err == nil {
		t.Error("bad client JSON accepted")
	}
	p := NewPKCE()
	u := AuthURL("https://auth.example", ClientConfig{ClientID: "id"}, "http://127.0.0.1:1/cb", "", p)
	if !strings.Contains(u, "gmail.readonly") || strings.Contains(u, "gmail.modify") || strings.Contains(u, "gmail.send") || !strings.Contains(u, "code_challenge_method=S256") {
		t.Errorf("unexpected scopes/PKCE: %s", u)
	}
}

// The app must have no Gmail write scope or code path.
func TestNoGmailWritePaths(t *testing.T) {
	for _, f := range []string{"clients.go", "auth.go", "writer.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"gmail.modify", "gmail.send", "gmail.compose", "mail.google.com/\"", "/send", "batchModify", "/trash", "/modify", "removeLabelIds", "addLabelIds"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s contains %q", f, bad)
			}
		}
	}
}

// The team's live sourcing tab (header row as provided, 13 Nov 2026): no
// Total Funding USD column, "Founder Name " with a trailing space, analyst
// assessment columns, and "source" / "Updates (sourcing sheet)".
var teamSourcingHeaders = strings.Split("Outreach status\tRelevance Flag\tCompany Name\tCountry\tSector\tBusiness Model\tStage\tFounded year\tHeadquarters\tWebsite\tCompany LinkedIn\tCompany Description\tProduct Descriptoon\tTarget Market\tTeam Size\tNumber of Customers\tNotable Customers\tRevenue Generated\tUnique Value Propsotions\tCompetitors\tVision\tFounder Name \tFounder LinkedIn\tFounder Emails\tDomain Knowledge\tTrack Record\tTechnical Knowledge\tSales Skills\tPre-money Evaluation\tAmount to be Raised\tLast Round\tNotable Investors\tIC Recommendation\tReason\tStatus\tExtra Notes\tsource\tUpdates (sourcing sheet)", "\t")

func TestOutboundRowForTeamSheet(t *testing.T) {
	if len(teamSourcingHeaders) != 38 {
		t.Fatalf("%d headers", len(teamSourcingHeaders))
	}
	rec, _ := json.Marshal(map[string]any{"name": "Acme", "hq_country": "Sri Lanka", "headquarters": "Colombo, Sri Lanka", "website": "https://acme.example", "founders": "A. Founder", "stage": "seed", "total_funding_usd": 1250000.0, "last_round": "Seed, 2025"})
	row := OutboundRow(teamSourcingHeaders, rec, "Partner X")
	col := map[string]string{}
	for i, h := range teamSourcingHeaders {
		col[h] = row[i]
	}
	want := map[string]string{"Outreach status": "Not Reached Out", "Company Name": "Acme", "Country": "Sri Lanka", "Business Model": "B2B", "Stage": "seed", "Headquarters": "Colombo, Sri Lanka",
		"Website": "https://acme.example", "Founder Name ": "A. Founder", "Last Round": "Seed, 2025", "Status": "Sourced", "IC Recommendation": "Potential Fit",
		"source": "nVentures Sourcing via Partner X", "Updates (sourcing sheet)": "Sourced via nVentures Sourcing (Go)"}
	for h, v := range want {
		if col[h] != v {
			t.Errorf("%q = %q, want %q", h, col[h], v)
		}
	}
	// Nothing is invented for analyst judgements, valuation, raise or contacts.
	for _, h := range []string{"Relevance Flag", "Founder Emails", "Domain Knowledge", "Track Record", "Technical Knowledge", "Sales Skills", "Pre-money Evaluation", "Amount to be Raised"} {
		if col[h] != "" {
			t.Errorf("%q should stay blank, got %q", h, col[h])
		}
	}
	// No funding column: the evidenced total goes into Extra Notes.
	if !strings.Contains(col["Extra Notes"], "Total funding to date (evidenced): USD 1,250,000.") {
		t.Errorf("extra notes: %q", col["Extra Notes"])
	}
	// With a funding column it is not repeated in the notes.
	row2 := OutboundRow(append([]string{"Total Funding USD"}, teamSourcingHeaders...), rec, "Partner X")
	if row2[0] != "1250000" || strings.Contains(row2[36], "Total funding") {
		t.Errorf("funding column: %q / %q", row2[0], row2[36])
	}
}
