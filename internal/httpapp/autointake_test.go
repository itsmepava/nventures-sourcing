package httpapp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

func TestAutoIntake(t *testing.T) {
	a, srv := newTestApp(t, nil)
	admin := adminClient(t, srv)
	responder := addMember(t, srv, admin, "Rae Responder", "responder@nventures-demo.test", "responder")
	ctx := store.WithActor(context.Background(), store.Actor{Name: "Auto-intake"})
	now := time.Now()

	// First check: the demo mailbox is new, so one import starts; promising
	// pitches go to the sheet, the rest wait in review.
	a.autoIntake(ctx, now)
	a.Jobs.Wait()
	js, _ := a.Store.Jobs(ctx, "inbound_import", 5)
	if len(js) != 1 || js[0].Params["auto_intake"] != true || js[0].Status != "completed" {
		t.Fatalf("first check: %+v", js)
	}
	if js[0].Counters["verified"] != float64(3) || js[0].Counters["review"] != float64(9) {
		t.Errorf("counters: %v", js[0].Counters)
	}
	if !strings.Contains(a.Store.GetSetting(ctx, "state.auto_intake_result", ""), "import #") {
		t.Errorf("result: %q", a.Store.GetSetting(ctx, "state.auto_intake_result", ""))
	}
	n := decode(t, do(t, responder, "GET", srv.URL+"/api/notifications", nil, ""))
	if !strings.Contains(strings.ToLower(fmtAny(n)), "3 added to the sheet, 9 need review") {
		t.Errorf("notification: %v", n)
	}

	// Within 5 minutes nothing happens; after 5 minutes with no new mail, no job is created.
	a.autoIntake(ctx, now.Add(time.Minute))
	a.autoIntake(ctx, now.Add(6*time.Minute))
	a.Jobs.Wait()
	if js, _ = a.Store.Jobs(ctx, "inbound_import", 5); len(js) != 1 {
		t.Fatalf("empty check created a job: %d", len(js))
	}
	if r := a.Store.GetSetting(ctx, "state.auto_intake_result", ""); r != "no new pitches" {
		t.Errorf("result: %q", r)
	}

	// A new pitch arrives and is picked up by the next check.
	a.Fixture.AddMail(fixtures.Mail{ID: "demo-m99", Thread: "demo-t-99", From: "Demo Forms <forms@nventures-demo.test>", Subject: "New pitch submission", Minutes: 99,
		Plain: "Company name: Coral Ledger (demo)\nWebsite: https://coralledger.nventures-demo.test\nHeadquarters: Colombo, Sri Lanka\nStage: Seed\nOne line on what you do: Accounts-receivable automation for small and medium businesses in Sri Lanka.\nContact email: hi@coralledger.nventures-demo.test\n"})
	a.autoIntake(ctx, now.Add(12*time.Minute))
	a.Jobs.Wait()
	js, _ = a.Store.Jobs(ctx, "inbound_import", 5)
	if len(js) != 2 || js[0].Counters["imported"] != float64(1) || js[0].Counters["verified"] != float64(1) {
		t.Fatalf("new mail: %+v", js[0].Counters)
	}
	if rows := a.Fixture.Rows(fixtures.InboundSheetID, "Pitches"); rows[len(rows)-1][0] != "Coral Ledger (demo)" || rows[len(rows)-1][4] != "" {
		t.Errorf("sheet row: %v", rows[len(rows)-1])
	}

	// Switched off: no checks.
	do(t, admin, "POST", srv.URL+"/api/settings", jsonHdr(), `{"auto_intake":false}`)
	before := a.Store.GetSetting(ctx, "state.auto_intake_at", "")
	a.autoIntake(ctx, now.Add(30*time.Minute))
	if a.Store.GetSetting(ctx, "state.auto_intake_at", "") != before {
		t.Error("checked while switched off")
	}
	if st := decode(t, do(t, admin, "GET", srv.URL+"/api/state", nil, "")); !strings.Contains(fmtAny(st), "every_minutes") {
		t.Error("status not in state")
	}
}

func fmtAny(v any) string { return strings.ReplaceAll(strings.ToLower(fmt.Sprint(v)), "\n", " ") }
