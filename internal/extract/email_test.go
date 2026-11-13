package extract_test

import (
	"strings"
	"testing"

	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
)

func byID() map[string]extract.Pitch {
	out := map[string]extract.Pitch{}
	for _, m := range fixtures.Mailbox() {
		out[m.ID] = extract.Extract(extract.FromGmail(m.Gmail()))
	}
	return out
}

func TestFixtureMailboxClassification(t *testing.T) {
	p := byID()
	want := map[string]string{
		// Added automatically: complete AND stated HQ in South Asia/Singapore AND
		// B2B or pre-seed/seed. Every other real pitch waits in review.
		"demo-m01": "ready", "demo-m02": "review", "demo-m03": "review", "demo-m04": "review", "demo-m05": "review",
		"demo-m06": "review", "demo-m07": "review", "demo-m08": "review", "demo-m09": "review", "demo-m10": "skipped",
		"demo-m11": "skipped", "demo-m12": "review", "demo-m13": "ready", "demo-m14": "ready", "demo-m15": "review", "demo-m16": "ready",
	}
	for id, status := range want {
		if p[id].Status != status {
			t.Errorf("%s: status %s want %s (%v) name=%q desc=%q", id, p[id].Status, status, p[id].Reasons, p[id].Name, p[id].Description)
		}
	}
	if p["demo-m01"].Name == p["demo-m02"].Name {
		t.Error("two companies in one thread collapsed")
	}
}

func TestHTMLOnlyAndBroadFunnel(t *testing.T) {
	p := byID()["demo-m03"]
	if p.Name != "Solace Grid (demo)" || !strings.Contains(p.Description, "Kenya") || p.Contact != "ceo@solacegrid.nventures-demo.test" {
		t.Fatalf("%+v", p)
	}
	if strings.Contains(p.Description, "alert") {
		t.Error("script text leaked into description")
	}
	if p.HQ != "" {
		t.Error("HQ invented")
	}
}

func TestReplyToForwardAndAttachments(t *testing.T) {
	p := byID()
	d := p["demo-m04"]
	if d.Contact != "founders@tidewell.nventures-demo.test" || d.ContactSource != "Reply-To header" || d.Sender != "ravi.menon@mail-demo.test" {
		t.Errorf("reply-to: %+v", d)
	}
	// The subject says "Pitch: Tidewell (demo)", but the name is never guessed.
	if d.Name != "" || d.NameGuessed || !strings.Contains(strings.Join(d.Reasons, " "), "Company name not stated") || len(d.Attachments) != 1 || d.Attachments[0].Reviewed {
		t.Errorf("direct pitch: %+v", d)
	}
	if strings.HasPrefix(d.Description, "Hi") || strings.Contains(d.Description, "Best,") || !strings.HasPrefix(d.Description, "Tidewell builds") {
		t.Errorf("greeting/sign-off kept: %q", d.Description)
	}
	if d.Website != "https://tidewell.nventures-demo.test" {
		t.Errorf("website %q", d.Website)
	}
	f := p["demo-m05"]
	if !f.Forwarded || f.Contact != "nila@mosaicclinic.nventures-demo.test" || f.HQ != "Kathmandu, Nepal" {
		t.Errorf("forward: %+v", f)
	}
}

func TestQuotedRepeatAndMalformedYear(t *testing.T) {
	p := byID()
	q := p["demo-m06"]
	if q.Structured || !q.FollowUp || strings.Contains(q.Description, "Accounts-payable") {
		t.Errorf("quoted repeat re-read: %+v", q)
	}
	y := p["demo-m07"]
	if y.IncorpYear != "20223" || !strings.Contains(y.Notes, "could not be parsed") {
		t.Errorf("year: %+v", y)
	}
}

func TestIncorporationIsNotHQAndNoInferredFunding(t *testing.T) {
	p := extract.Extract(extract.Input{Text: "Company name: Polar Labs\nWebsite: https://polar.example\nCountry of incorporation: Canada\nStage: Series A\nOne line on what you do: Consumer software startup building a new way to share creative projects. Raising $9 million.\nContact email: founder@polar.example"})
	// Complete, but incorporation isn't HQ: it waits in review, and the only
	// reason is the focus check (nothing about it is rejected).
	if p.Status != "review" || p.HQ != "" || !strings.Contains(p.Notes, "Canada") || p.Name != "Polar Labs" || len(p.Reasons) != 1 || !strings.Contains(p.Reasons[0], "HQ not stated") {
		t.Fatalf("%+v", p)
	}
	q := byID()["demo-m09"]
	if strings.Contains(strings.Join(q.Reasons, " "), "established") {
		t.Errorf("exited founder / enterprise customers flagged: %v", q.Reasons)
	}
	for _, s := range []string{q.Notes} {
		if strings.Contains(s, "40M") && strings.Contains(s, "Funding") {
			t.Error("founder exit inferred as funding")
		}
	}
}

func TestInjectionIgnored(t *testing.T) {
	p := byID()["demo-m12"]
	if p.Status != "review" || !strings.Contains(strings.Join(p.Reasons, " "), "ignored") {
		t.Fatalf("%+v", p)
	}
}

func TestUnstructuredAndTestSkipped(t *testing.T) {
	if extract.Extract(extract.Input{Text: "Please see our deck", From: "a@example.com"}).Status != "review" {
		t.Error("unstructured should review")
	}
	big := extract.Extract(extract.Input{Text: "Company name: Big Co\nOne line on what you do: A publicly traded multinational operating across many countries.\nContact email: a@example.com"})
	if big.Status != "review" {
		t.Error("established should review")
	}
	tc := extract.Extract(extract.Input{Text: "Company name: Test Company\nOne line on what you do: This is a test form for this application only.\nCountry of incorporation: 2025\nContact email: test@example.com"})
	if tc.Status != "skipped" || tc.HQ != "" {
		t.Errorf("%+v", tc)
	}
}

func TestLatin1AndNestedMIME(t *testing.T) {
	var part extract.GmailPart
	part.MimeType = "multipart/mixed"
	child := extract.GmailPart{MimeType: "text/plain", Headers: []extract.GmailHeader{{Name: "Content-Type", Value: "text/plain; charset=iso-8859-1"}}}
	child.Body.Data = "Q29tcGFueSBuYW1lOiBDYWbp" // "Company name: Café" in latin-1
	part.Parts = []extract.GmailPart{{MimeType: "multipart/alternative", Parts: []extract.GmailPart{child}}}
	if got := extract.BodyText(part); !strings.Contains(got, "Café") {
		t.Fatalf("%q", got)
	}
}

func TestPromisingRule(t *testing.T) {
	base := "Company name: Acme Ledger\nWebsite: https://acme.example\nContact email: a@acme.example\nOne line on what you do: "
	cases := []struct {
		text, status, hq, gap string
	}{
		{base + "Invoice automation for small and medium businesses in the region.\nHeadquarters: Colombo, Sri Lanka\nStage: Seed", "ready", "Colombo, Sri Lanka", ""},
		// The sender's own HQ sentence counts; markets do not.
		{base + "We're headquartered in Dhaka, Bangladesh and sell to garment exporters.\nStage: Pre-seed", "ready", "Dhaka, Bangladesh", ""},
		{base + "Software for garment exporters in Bangladesh.\nStage: Pre-seed", "review", "", "HQ not stated"},
		{base + "A consumer app for planning weekend trips with friends.\nHeadquarters: Austin, USA\nStage: Seed", "review", "Austin, USA", "HQ outside South Asia/Singapore"},
		{base + "A photo-sharing app for teenagers and their families.\nHeadquarters: Singapore\nStage: Series B", "review", "Singapore", "no B2B focus or pre-seed/seed stage"},
		{base + "Scheduling SaaS for independent clinics and pharmacies.\nSector: B2B SaaS\nHeadquarters: Kathmandu, Nepal\nStage: Series A", "ready", "Kathmandu, Nepal", ""},
		{"Company name: NoSite\nContact email: a@nosite.example\nOne line on what you do: Payments and payouts for SMEs across India and Nepal.\nHeadquarters: Pune, India\nStage: Seed", "review", "Pune, India", "no website stated"},
	}
	for _, c := range cases {
		p := extract.Extract(extract.Input{Text: c.text})
		if p.Status != c.status || p.HQ != c.hq || (c.gap != "" && !strings.Contains(strings.Join(p.Reasons, " "), c.gap)) {
			t.Errorf("%q\n got status=%s hq=%q reasons=%v", c.text, p.Status, p.HQ, p.Reasons)
		}
	}
	if p := extract.Extract(extract.Input{Text: base[:len(base)-len("One line on what you do: ")] + "\nOne line on what you do: x"}); p.Name != "Acme Ledger" {
		t.Error("labelled name lost")
	}
}
