package httpapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/itsmepava/nventures-sourcing/internal/config"
	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

func partnerNamed(t *testing.T, a *App, name string) store.Partner {
	t.Helper()
	ps, _ := a.Store.Partners(context.Background(), false, 0)
	for _, p := range ps {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("partner %q not found", name)
	return store.Partner{}
}

func TestFindPartnerWebsite(t *testing.T) {
	a, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	sourcer := addMember(t, srv, admin, "Sam Sourcer", "sourcer@nventures-demo.test", "sourcer")
	viewer := addMember(t, srv, admin, "Vic Viewer", "viewer@nventures-demo.test", "viewer")
	if r := do(t, sourcer, "POST", srv.URL+"/api/partners", jsonHdr(), `{"name":"Indus Bridge Capital (demo)"}`); r.StatusCode != 201 && r.StatusCode != 200 {
		t.Fatalf("add partner: %d", r.StatusCode)
	}
	p := partnerNamed(t, a, "Indus Bridge Capital (demo)")
	if p.Website != "" {
		t.Fatalf("expected no website, got %q", p.Website)
	}
	path := fmt.Sprintf("%s/api/partners/%d/find-site", srv.URL, p.ID)
	if r := do(t, viewer, "POST", path, jsonHdr(), ""); r.StatusCode != 403 {
		t.Fatalf("viewer: %d", r.StatusCode)
	}
	m := decode(t, do(t, sourcer, "POST", path, jsonHdr(), ""))
	res, _ := m["result"].(map[string]any)
	want := "https://indusbridge" + fixtures.DemoSuffix + "/"
	if res["url"] != want || res["source"] != "guess" {
		t.Fatalf("find: %v", m)
	}
	p = partnerNamed(t, a, "Indus Bridge Capital (demo)")
	if p.Website != want || p.WebsiteSource != "guess" || p.WebsiteNote == "" || p.SiteCheckedAt == "" {
		t.Fatalf("stored: %+v", p)
	}
	// Confirming (saving the same address) marks it as entered by a person; a
	// confirmed website is never replaced by a lookup.
	do(t, sourcer, "PATCH", fmt.Sprintf("%s/api/partners/%d", srv.URL, p.ID), jsonHdr(), fmt.Sprintf(`{"website":%q}`, want))
	if p = partnerNamed(t, a, "Indus Bridge Capital (demo)"); p.WebsiteSource != "manual" {
		t.Fatalf("confirm: %+v", p)
	}
	if r := do(t, sourcer, "POST", path, jsonHdr(), ""); r.StatusCode != 409 {
		t.Errorf("lookup over a confirmed website: %d", r.StatusCode)
	}
	// The partner list JSON never exposes the contact hint.
	r := do(t, viewer, "GET", srv.URL+"/api/partners", nil, "")
	var raw []map[string]any
	json.NewDecoder(r.Body).Decode(&raw)
	for _, x := range raw {
		if _, ok := x["contact_hint"]; ok {
			t.Fatal("contact hint exposed")
		}
	}
}

func TestSourcingRunFindsMissingPartnerSites(t *testing.T) {
	a, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	ctx := context.Background()
	// One partner that can be found by name, one with only a work email, one that can't be found.
	a.Store.MergePartner(ctx, "Indus Bridge Capital (demo)", "", true, "", 0, "")
	a.Store.MergePartner(ctx, "Harbour Angels (demo)", "", true, "", 0, "Asha Perera <asha@bengalangels"+fixtures.DemoSuffix+">")
	a.Store.MergePartner(ctx, "Nowhere Fund (demo)", "", true, "", 0, "someone@gmail.com")
	do(t, admin, "POST", srv.URL+"/api/sourcing/run", jsonHdr(), `{"target":50,"max_partners":20}`)
	a.Jobs.Wait()
	js, _ := a.Store.Jobs(ctx, "outbound_run", 1)
	if js[0].Counters["partner_sites_found"] != float64(2) || js[0].Counters["partner_sites_missing"] != float64(2) { // plus the demo partner that deliberately has no website
		t.Fatalf("counters: %v", js[0].Counters)
	}
	if p := partnerNamed(t, a, "Harbour Angels (demo)"); p.WebsiteSource != "email" || !strings.Contains(p.Website, "bengalangels") {
		t.Errorf("email hint: %+v", p)
	}
	if p := partnerNamed(t, a, "Indus Bridge Capital (demo)"); p.WebsiteSource != "guess" || p.RunCount != 1 {
		t.Errorf("guess: %+v", p)
	}
	// The failed lookup is remembered: the next run does not repeat it.
	do(t, admin, "POST", srv.URL+"/api/sourcing/run", jsonHdr(), `{"target":50,"max_partners":20}`)
	a.Jobs.Wait()
	js, _ = a.Store.Jobs(ctx, "outbound_run", 1)
	if js[0].Counters["partner_sites_missing"] != nil {
		t.Errorf("lookup repeated within a week: %v", js[0].Counters)
	}
	ev, _ := a.Store.Events(ctx, js[0].ID, "partner", 0, 50)
	seen := false
	for _, e := range ev {
		seen = seen || strings.Contains(e.Message, "Nowhere Fund (demo): no website (last lookup found none")
	}
	if !seen {
		t.Error("skip not reported")
	}
}

func TestPartnerSearchSetting(t *testing.T) {
	a, srv := newTestApp(t, func(c *config.Config) { c.Mode = "live" })
	admin := adminClient(t, srv)
	if r := do(t, admin, "POST", srv.URL+"/api/settings", jsonHdr(), `{"partner_search":true}`); r.StatusCode != 400 {
		t.Fatalf("enabled without a key: %d", r.StatusCode)
	}
	if a.SiteFinder(context.Background()).Search != nil {
		t.Fatal("search configured without a key")
	}
	a2, srv2 := newTestApp(t, func(c *config.Config) { c.Mode = "live"; c.TavilyKey = "tvly-test"; c.TavilyKeyPresent = true })
	admin2 := adminClient(t, srv2)
	if a2.SiteFinder(context.Background()).Search != nil {
		t.Fatal("search on by default")
	}
	if r := do(t, admin2, "POST", srv2.URL+"/api/settings", jsonHdr(), `{"partner_search":true}`); r.StatusCode != 200 {
		t.Fatalf("enable: %d", r.StatusCode)
	}
	if a2.SiteFinder(context.Background()).Search == nil {
		t.Fatal("search not enabled")
	}
	body := fmt.Sprint(decode(t, do(t, admin2, "GET", srv2.URL+"/api/settings", nil, "")), decode(t, do(t, admin2, "GET", srv2.URL+"/api/state", nil, "")))
	if strings.Contains(body, "tvly-test") {
		t.Fatal("API exposed the Tavily key")
	}
}
