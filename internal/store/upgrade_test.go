package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// A database created by v0.3.0-alpha.1 (schema 1) keeps every record when
// opened by the current version, and gains the new columns.
func TestUpgradeFromSchemaOnePreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := migrationFS.ReadFile("migrations/0001_init.sql")
	stmts := []string{`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`, string(v1), `INSERT INTO schema_migrations VALUES(1,'0001_init.sql','2026-11-12T00:00:00Z')`,
		`INSERT INTO companies(name,name_key,domain,data,partner,created_at,updated_at,sync_status,sheet_row,sheet_range,starred,demo) VALUES('Old Co','oldco','old.example','{"name":"Old Co","total_funding_usd":500000}','P','t','t','verified',4,'''Active Sourcing''!A4:AL4',1,1)`,
		`INSERT INTO pitches(message_id,account,mode,name,status,data,created_at,updated_at) VALUES('a:m1','a','demo','Old Pitch','verified','{"name":"Old Pitch","sheet_row":3}','t','t')`,
		`INSERT INTO jobs(kind,mode,status,started_at) VALUES('outbound_run','demo','completed','t')`,
		`INSERT INTO settings(key,value,updated_at) VALUES('pref.theme','dark','t')`}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q[:30], err)
		}
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if v := st.SchemaVersion(ctx); v < 2 {
		t.Fatalf("schema %d", v)
	}
	c, err := st.Company(ctx, 1)
	if err != nil || c.Name != "Old Co" || *c.SheetRow != 4 || !c.Starred || c.SheetURL != "" || string(c.Data) != `{"name":"Old Co","total_funding_usd":500000}` {
		t.Fatalf("%+v %v", c, err)
	}
	p, err := st.Pitch(ctx, 1)
	if err != nil || p.Status != "verified" {
		t.Fatalf("%+v %v", p, err)
	}
	if st.GetSetting(ctx, "pref.theme", "") != "dark" {
		t.Error("preference lost")
	}
	if js, _ := st.Jobs(ctx, "", 5); len(js) != 1 {
		t.Error("job history lost")
	}
	// Re-opening is a no-op.
	st.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
}
