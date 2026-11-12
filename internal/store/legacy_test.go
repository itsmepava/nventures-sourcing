package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
)

// buildPythonFixture creates a fictional database with the exact Python schema.
func buildPythonFixture(t *testing.T, path string, demo bool) {
	t.Helper()
	schema, err := os.ReadFile("testdata/python_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	headers := fixtures.OutboundHeaders
	full := map[string]any{"demo": demo, "name": "Fixture Ledger", "hq_country": "Sri Lanka", "headquarters": "Colombo", "stage": "seed", "b2b": true, "active": true, "total_funding_usd": 800000,
		"evidence": map[string]any{"headquarters": []map[string]string{{"url": "https://fixture-ledger.example", "quote": "Fixture Ledger is headquartered in Sri Lanka."}}},
		"sources":  []map[string]string{{"url": "https://fixture-ledger.example", "title": "Fixture", "content": "x"}}, "uvp": "Unicode ✓ — කෘෂි", "notes": "line1\nline2"}
	for _, h := range headers {
		if _, ok := full[strings.ToLower(h)]; !ok {
			full["col_"+h] = "value for " + h
		}
	}
	data, _ := json.Marshal(full)
	stmts := []string{
		`INSERT INTO alembic_version VALUES('0003')`,
		`INSERT INTO partners VALUES(1,'Fixture Angels','https://fixture-angels.example',1,'2026-11-10T01:02:03+00:00',4,2)`,
		`INSERT INTO partners VALUES(2,'Excluded Fund','',0,'',0,0)`,
		`INSERT INTO runs VALUES(1,'2026-11-10T01:00:00+00:00','2026-11-10T01:05:00+00:00','demo','completed_with_errors',10,6,3,4,2,1,9,100,0,'3/10 accepted',1,2,1)`,
		`INSERT INTO runs VALUES(2,'2026-11-11T01:00:00+00:00',NULL,'demo','running',5,2,0,0,0,0,0,40,1,'Researching',0,0,0)`,
		`INSERT INTO events VALUES(1,1,'2026-11-10T01:00:01+00:00','started','Run started','{}')`,
		`INSERT INTO events VALUES(2,1,'2026-11-10T01:03:00+00:00','sync_error','Fixture Two: Sheets timeout','{"company_id": 2}')`,
		`INSERT INTO events VALUES(3,NULL,'2026-11-10T02:00:00+00:00','inbox_review','Pitch 1: ready','{}')`,
		`INSERT INTO companies VALUES(1,'Fixture Ledger','fixtureledger','fixture-ledger.example','linkedin.com/company/fixture-ledger',?,'Fixture Angels','2026-11-10T01:02:00+00:00','verified',7,'''Active Sourcing''!A7:AL7',NULL)`,
		`INSERT INTO companies VALUES(2,'Fixture Two','fixturetwo',NULL,NULL,?,'Fixture Angels','2026-11-10T01:03:00+00:00','error',NULL,NULL,'Sheets timeout; reconcile before retry')`,
		`INSERT INTO pitches VALUES(1,'demo:demo-form-1','demo','Edited Pitch Name','demo_verified','{"name":"Edited Pitch Name","description":"Edited description","contact":"a@fixture.example","hq":"","website":"https://fixture-pitch.example","notes":"manual edit","status":"ready","sheet_row":4}','2026-11-10T03:00:00+00:00','')`,
		`INSERT INTO pitches VALUES(2,'demo:demo-direct-1','demo','Pitch for review','review','{"name":"Pitch for review","description":"Thin","contact":"","hq":"","website":"","notes":""}','2026-11-10T03:01:00+00:00','Import or verification failed.')`,
		`INSERT INTO inbox_jobs VALUES(1,'completed','{"processed":2,"verified":1,"review":1,"skipped":0,"duplicates":0,"errors":0,"message":"Processed 2 of 2 messages","demo":true,"more_available":false}','2026-11-10T03:00:00+00:00')`,
		`INSERT INTO inbox_jobs VALUES(2,'running','{"demo":true,"message":"Starting"}','2026-11-11T03:00:00+00:00')`,
	}
	two, _ := json.Marshal(map[string]any{"demo": demo, "name": "Fixture Two"})
	for _, q := range stmts {
		var err error
		switch {
		case strings.Contains(q, "'Fixture Ledger'"):
			_, err = db.Exec(q, string(data))
		case strings.Contains(q, "'Fixture Two','fixturetwo'"):
			_, err = db.Exec(q, string(two))
		default:
			_, err = db.Exec(q)
		}
		if err != nil {
			t.Fatalf("%s: %v", q[:40], err)
		}
	}
}

func TestMigratePythonFixtureIdempotentAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "python.db")
	buildPythonFixture(t, src, true)
	before := hashSet(src)
	st, err := Open(filepath.Join(dir, "go", "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	rep, err := st.MigratePython(ctx, src, "demo", filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.SourceUnchanged || rep.AlembicVersion != "0003" {
		t.Fatalf("%+v", rep)
	}
	for _, c := range rep.Checks {
		if strings.Contains(c, "MISMATCH") {
			t.Error(c)
		}
	}
	want := map[string]int{"partners": 2, "jobs": 4, "events": 3, "companies": 2, "pitches": 2}
	for k, v := range want {
		if rep.Imported[k] != v {
			t.Errorf("%s imported %d want %d", k, rep.Imported[k], v)
		}
	}
	c, _ := st.Company(ctx, 1)
	var d map[string]any
	json.Unmarshal(c.Data, &d)
	if c.SheetRow == nil || *c.SheetRow != 7 || c.SheetRange != "'Active Sourcing'!A7:AL7" || c.SyncStatus != "verified" || d["uvp"] != "Unicode ✓ — කෘෂි" || d["notes"] != "line1\nline2" || len(d) < 38 {
		t.Errorf("company fields: %+v %v", c, len(d))
	}
	two, _ := st.Company(ctx, 2)
	if two.SyncError != "Sheets timeout; reconcile before retry" || two.SyncStatus != "error" {
		t.Errorf("sync error lost: %+v", two)
	}
	ps, _ := st.Partners(ctx, false, 0)
	if len(ps) != 2 || ps[1].RunCount != 4 || ps[1].LastSourced != "2026-11-10T01:02:03+00:00" || ps[0].Relevant {
		t.Errorf("partners %+v", ps)
	}
	jobs, _ := st.Jobs(ctx, "", 10)
	interrupted := 0
	for _, j := range jobs {
		if j.Status == "interrupted" {
			interrupted++
		}
		if j.LegacyRef == "py:runs:1" && (j.Counters["verified"].(float64) != 2 || j.Counters["sync_failed"].(float64) != 1) {
			t.Errorf("run counters %+v", j.Counters)
		}
	}
	if interrupted != 2 {
		t.Errorf("interrupted %d", interrupted)
	}
	evs, _ := st.Events(ctx, 0, "sync_error", 0, 10)
	if len(evs) != 1 || evs[0].JobID == nil {
		t.Errorf("events %+v", evs)
	}
	p, _ := st.PitchByMessage(ctx, "demo:demo-form-1")
	if p.Name != "Edited Pitch Name" || p.Status != "demo_verified" || !strings.Contains(string(p.Data), "manual edit") {
		t.Errorf("pitch %+v", p)
	}
	rep2, err := st.MigratePython(ctx, src, "demo", filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep2.Imported) != 0 || rep2.AlreadyPresent["companies"] != 2 || rep2.TargetBackup == "" {
		t.Errorf("not idempotent: %+v", rep2)
	}
	if _, err := os.Stat(rep2.TargetBackup); err != nil {
		t.Error("backup missing")
	}
	after := hashSet(src)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("source %s changed", k)
		}
	}
	// Mode guard: demo data is never imported into the live store.
	live, _ := Open(filepath.Join(dir, "go", "live.db"))
	defer live.Close()
	if _, err := live.MigratePython(ctx, src, "live", ""); err == nil {
		t.Error("demo source imported into live store")
	}
}
