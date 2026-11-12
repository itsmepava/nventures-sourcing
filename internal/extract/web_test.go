package extract

import (
	"reflect"
	"strings"
	"testing"
)

func TestSentencesKeepDecimalsAndAbbreviations(t *testing.T) {
	got := Sentences("FactorySignal has raised a total of USD 1.2 million in funding. Acme Pvt. Ltd. is headquartered in Pune, India. Visit acme.example.com today! It raised US$ 950,000 in total.")
	want := []string{"FactorySignal has raised a total of USD 1.2 million in funding.", "Acme Pvt. Ltd. is headquartered in Pune, India.", "Visit acme.example.com today!", "It raised US$ 950,000 in total."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestResearchFundingAmountsExact(t *testing.T) {
	doc, _ := ParseHTML(strings.NewReader(`<p>Pune-based. FactorySignal is headquartered in Pune, India.</p><p>FactorySignal provides software for manufacturers.</p><p>FactorySignal has raised a total of USD 1.2 million in funding.</p>`), nil)
	r := Research("FactorySignal", "https://f.example", []Page{{URL: "https://f.example/", Doc: doc}})
	if r.TotalFundingUSD == nil || *r.TotalFundingUSD != 1.2e6 {
		t.Fatalf("funding %v", r.TotalFundingUSD)
	}
	if q := r.Evidence["funding"][0].Quote; q != "FactorySignal has raised a total of USD 1.2 million in funding." {
		t.Fatalf("quote %q", q)
	}
}
