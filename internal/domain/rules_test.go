package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

func sample() Record {
	url := "https://ledgerleaf.example"
	return Record{
		Name: "LedgerLeaf", Website: url, HQCountry: "Sri Lanka", Headquarters: "Colombo, Sri Lanka",
		Stage: "seed", B2B: Bool(true), Active: Bool(true), TotalFundingUSD: Float(800000), FundingClaimsUSD: []float64{800000},
		Evidence: map[string][]Evidence{
			"headquarters": {{url, "LedgerLeaf is headquartered in Sri Lanka."}},
			"b2b":          {{url, "LedgerLeaf sells workflow software to businesses."}},
			"active":       {{url, "LedgerLeaf operates an active software product with paying customers."}},
			"stage":        {{url, "LedgerLeaf closed its seed financing round."}},
			"funding":      {{url, "LedgerLeaf has raised a total of USD 800000."}},
		},
	}
}

func clone(r Record) Record {
	b, _ := json.Marshal(r)
	var out Record
	_ = json.Unmarshal(b, &out)
	return out
}

func TestEachEligibleCountryAtBoundary(t *testing.T) {
	for _, c := range Countries {
		r := clone(sample())
		r.HQCountry, r.Headquarters = c, c
		r.Evidence["headquarters"][0].Quote = "Test company is headquartered in " + c + "."
		r.TotalFundingUSD = Float(3000000)
		r.Evidence["funding"][0].Quote = "Test company has raised a total of USD 3000000."
		if got := Qualifies(r); len(got) != 0 {
			t.Errorf("%s: %v", c, got)
		}
	}
}

func TestUnknownOutsideAndNationalityFail(t *testing.T) {
	for _, v := range []string{"", "United States", "United Kingdom", "Unknown"} {
		r := clone(sample())
		r.HQCountry = v
		if len(Qualifies(r)) == 0 {
			t.Errorf("country %q accepted", v)
		}
	}
	r := clone(sample())
	r.Headquarters = ""
	if len(Qualifies(r)) == 0 {
		t.Error("blank headquarters accepted")
	}
	for _, q := range []string{
		"Headquartered in San Francisco. Its founders are from India.",
		"Headquartered in San Francisco with operations in India.",
		"Based in London, serving customers in India.",
		"Headquartered in Dubai with a sales office in Singapore.",
		"Its founders are Indian and its target market is India.",
	} {
		r := clone(sample())
		r.HQCountry = "India"
		r.Evidence["headquarters"][0].Quote = q
		if len(Qualifies(r)) == 0 {
			t.Errorf("accepted on %q", q)
		}
	}
	outside := clone(sample())
	outside.HQCountry, outside.Headquarters = "United States", "New York"
	outside.Evidence["headquarters"][0].Quote = "LedgerLeaf is headquartered in New York, United States."
	if !Contradiction(Qualifies(outside)) {
		t.Error("evidenced outside HQ should be a contradiction")
	}
}

func TestFundingStageTypesFailClosed(t *testing.T) {
	for _, v := range []*float64{Float(3000001), nil, Float(-1), Float(math.NaN()), Float(math.Inf(1))} {
		r := clone(sample())
		r.TotalFundingUSD = v
		if len(Qualifies(r)) == 0 {
			t.Errorf("funding %v accepted", v)
		}
	}
	for _, s := range []string{"series a", "series b", "growth", "unknown", ""} {
		r := clone(sample())
		r.Stage = s
		if len(Qualifies(r)) == 0 {
			t.Errorf("stage %q accepted", s)
		}
	}
	for _, b := range []*bool{Bool(false), nil} {
		r := clone(sample())
		r.B2B = b
		if len(Qualifies(r)) == 0 {
			t.Error("b2b not true accepted")
		}
	}
	r := clone(sample())
	r.Active = Bool(false)
	if len(Qualifies(r)) == 0 {
		t.Error("inactive accepted")
	}
	// JSON with wrong types cannot unmarshal into a qualifying record.
	var bad Record
	if err := json.Unmarshal([]byte(`{"name":"X","b2b":"true"}`), &bad); err == nil {
		t.Error("string b2b should not decode")
	}
}

func TestIndependentMaturityAndFundingVeto(t *testing.T) {
	cases := map[string]func(*Record){
		"series a quote": func(r *Record) { r.Evidence["stage"][0].Quote = "Company closed its Series A round in 2025." },
		"21m excerpt":    func(r *Record) { r.Evidence["funding"][0].Quote = "Company raised a total of USD 21 million." },
		"claims":         func(r *Record) { r.FundingClaimsUSD = []float64{21000000} },
		"no evidence":    func(r *Record) { r.Evidence = nil },
		"undisclosed":    func(r *Record) { r.Evidence["funding"][0].Quote = "Funding has not been disclosed publicly." },
		"latest round": func(r *Record) {
			r.Evidence["funding"][0].Quote = "Company raised USD 800000 in its latest seed round."
		},
		"sgd":             func(r *Record) { r.Evidence["funding"][0].Quote = "Company raised a total of S$800000." },
		"acquired":        func(r *Record) { r.Evidence["stage"][0].Quote = "The seed company was acquired by a bank last year." },
		"amount mismatch": func(r *Record) { r.TotalFundingUSD = Float(500000) },
	}
	for name, mutate := range cases {
		r := clone(sample())
		mutate(&r)
		if len(Qualifies(r)) == 0 {
			t.Errorf("%s accepted", name)
		}
	}
	// Evidenced bootstrapped zero is the one case where 0 is real.
	r := clone(sample())
	r.Stage = "bootstrapped"
	r.Evidence["stage"][0].Quote = "LedgerLeaf is a bootstrapped company."
	r.TotalFundingUSD = Float(0)
	r.FundingClaimsUSD = nil
	r.Evidence["funding"][0].Quote = "LedgerLeaf has no external funding and is self-funded."
	if got := Qualifies(r); len(got) != 0 {
		t.Errorf("bootstrapped zero rejected: %v", got)
	}
}

func TestDuplicateNormalization(t *testing.T) {
	known := []Identity{{NameKey: NameKey("Acme Private Limited"), Domain: DomainKey("https://www.acme.com/path?x=1"), LinkedInKey: LinkedInKey("https://www.linkedin.com/company/acme/")}}
	for i, id := range []Identity{IdentityOf("ACME, Inc.", "", ""), IdentityOf("Different", "acme.com", ""), IdentityOf("Different", "", "https://linkedin.com/company/ACME?x=1")} {
		if !Duplicate(id, known) {
			t.Errorf("case %d not duplicate", i)
		}
	}
	if Duplicate(IdentityOf("New", "", ""), known) {
		t.Error("new flagged duplicate")
	}
	// Shared hosts never become an identity.
	a := IdentityOf("Alpha", "https://docs.google.com/presentation/d/1", "")
	b := IdentityOf("Beta", "https://docs.google.com/presentation/d/2", "")
	if a.Matches(b) {
		t.Error("shared document host merged two businesses")
	}
	if IdentityDomain("https://company.notion.site/x") != "" {
		t.Error("notion subdomain should be shared")
	}
}

func TestUSDAmounts(t *testing.T) {
	got := ParseUSDAmounts("raised a total of US$ 1.5 million and $200k, then USD 2,000,000")
	want := []float64{1.5e6, 2e5, 2e6}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%v", got)
	}
}
