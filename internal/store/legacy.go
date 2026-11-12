package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
)

// MigrationReport records what a Python database import did.
type MigrationReport struct {
	Source          string            `json:"source"`
	SourceSHA256    map[string]string `json:"source_sha256"`
	SourceUnchanged bool              `json:"source_unchanged"`
	AlembicVersion  string            `json:"alembic_version"`
	Mode            string            `json:"mode"`
	SourceCounts    map[string]int    `json:"source_counts"`
	Imported        map[string]int    `json:"imported"`
	AlreadyPresent  map[string]int    `json:"already_present"`
	Skipped         []string          `json:"skipped"`
	Checks          []string          `json:"checks"`
	TargetBackup    string            `json:"target_backup,omitempty"`
	StartedAt       string            `json:"started_at"`
	FinishedAt      string            `json:"finished_at"`
}

func fileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func hashSet(path string) map[string]string {
	out := map[string]string{}
	for _, p := range []string{path, path + "-wal"} {
		if h := fileHash(p); h != "" {
			out[p] = h
		}
	}
	return out
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case int64:
		return fmt.Sprint(t)
	case float64:
		return fmt.Sprint(t)
	default:
		return fmt.Sprint(t)
	}
}

func num(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	case string:
		var n int64
		fmt.Sscan(t, &n)
		return n
	case []byte:
		var n int64
		fmt.Sscan(string(t), &n)
		return n
	}
	return 0
}

func jsonText(v any, def string) string {
	s := str(v)
	if s == "" || !json.Valid([]byte(s)) {
		return def
	}
	return s
}

// MigratePython imports a recovered Python/SQLAlchemy database into this
// store. The source is opened read-only and its file hashes are compared
// before and after. The import is one transaction and idempotent: every
// imported row carries a legacy_ref, so repeating it imports nothing new.
func (s *Store) MigratePython(ctx context.Context, src, mode string, backupDir string) (MigrationReport, error) {
	rep := MigrationReport{Source: src, Mode: mode, SourceCounts: map[string]int{}, Imported: map[string]int{}, AlreadyPresent: map[string]int{}, StartedAt: Now()}
	if _, err := os.Stat(src); err != nil {
		return rep, fmt.Errorf("source database not found")
	}
	rep.SourceSHA256 = hashSet(src)
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(10000)")
	srcDB, err := sql.Open("sqlite", "file:"+src+"?"+q.Encode())
	if err != nil {
		return rep, err
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	// Read everything inside one read transaction for a consistent snapshot.
	rtx, err := srcDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return rep, err
	}
	defer rtx.Rollback()
	_ = rtx.QueryRowContext(ctx, `SELECT version_num FROM alembic_version`).Scan(&rep.AlembicVersion)
	tables := map[string][]map[string]any{}
	for _, t := range []string{"partners", "runs", "events", "companies", "pitches", "inbox_jobs"} {
		var exists int
		_ = rtx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, t).Scan(&exists)
		if exists == 0 {
			rep.Skipped = append(rep.Skipped, "table "+t+" absent in source (older schema)")
			continue
		}
		rows, err := rowsAsMapsTx(ctx, rtx, t)
		if err != nil {
			return rep, fmt.Errorf("read %s: %w", t, err)
		}
		tables[t] = rows
		rep.SourceCounts[t] = len(rows)
	}
	_ = rtx.Rollback()
	for _, c := range tables["companies"] {
		var d map[string]any
		_ = json.Unmarshal([]byte(str(c["data"])), &d)
		isDemo, _ := d["demo"].(bool)
		if isDemo != (mode == "demo") {
			return rep, fmt.Errorf("source contains %s records; import it into the %s data store instead", map[bool]string{true: "demo", false: "live"}[isDemo], map[bool]string{true: "demo", false: "live"}[isDemo])
		}
	}
	for _, p := range tables["pitches"] {
		if m := str(p["mode"]); m != "" && m != mode {
			return rep, fmt.Errorf("source contains %s pitches; import it into the %s data store instead", m, m)
		}
	}
	if backupDir != "" {
		var n int
		_ = s.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM companies)+(SELECT COUNT(*) FROM pitches)+(SELECT COUNT(*) FROM jobs)`).Scan(&n)
		if n > 0 {
			rep.TargetBackup = fmt.Sprintf("%s/pre-migration-%s.db", backupDir, time.Now().UTC().Format("20060102-150405"))
			if err := s.Backup(ctx, rep.TargetBackup); err != nil {
				return rep, fmt.Errorf("pre-migration backup failed: %w", err)
			}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	exec := func(table, ref, query string, args ...any) (int64, error) {
		var existing int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE legacy_ref=?`, ref).Scan(&existing); err == nil {
			rep.AlreadyPresent[table]++
			return existing, nil
		}
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s %s: an existing record has the same identity; kept the existing one", table, ref))
				return 0, nil
			}
			return 0, fmt.Errorf("%s %s: %w", table, ref, err)
		}
		rep.Imported[table]++
		return res.LastInsertId()
	}
	for _, p := range tables["partners"] {
		ref := fmt.Sprintf("py:partners:%d", num(p["id"]))
		if _, err := exec("partners", ref, `INSERT INTO partners(name,website,relevant,last_sourced,run_count,last_added,created_at,legacy_ref) VALUES(?,?,?,?,?,?,?,?)`,
			str(p["name"]), str(p["website"]), num(p["relevant"]), str(p["last_sourced"]), num(p["run_count"]), num(p["last_added"]), Now(), ref); err != nil {
			return rep, err
		}
	}
	runMap := map[int64]int64{}
	for _, r := range tables["runs"] {
		ref := fmt.Sprintf("py:runs:%d", num(r["id"]))
		status := str(r["status"])
		if status == "running" || status == "queued" {
			status = "interrupted"
		}
		counters := map[string]any{"target": num(r["target"]), "accepted": num(r["accepted"]), "rejected": num(r["rejected"]), "duplicates": num(r["duplicates"]), "errors": num(r["errors"]), "researched": num(r["researched"]), "verified": num(r["synced"]), "sync_failed": num(r["sync_errors"])}
		params := map[string]any{"target": num(r["target"]), "max_partners": num(r["max_partners"]), "sync": num(r["sync_to_sheets"]) == 1, "legacy": true}
		id, err := exec("jobs", ref, `INSERT INTO jobs(kind,mode,status,params,counters,progress,message,cancel_requested,started_at,finished_at,legacy_ref) VALUES('outbound_run',?,?,?,?,?,?,?,?,?,?)`,
			str(r["mode"]), status, js(params), js(counters), num(r["progress"]), str(r["message"]), num(r["cancel_requested"]), str(r["started_at"]), nullStr(str(r["finished_at"])), ref)
		if err != nil {
			return rep, err
		}
		runMap[num(r["id"])] = id
	}
	for _, j := range tables["inbox_jobs"] {
		ref := fmt.Sprintf("py:inbox_jobs:%d", num(j["id"]))
		status := str(j["status"])
		if status == "running" {
			status = "interrupted"
		}
		var data map[string]any
		_ = json.Unmarshal([]byte(jsonText(j["data"], "{}")), &data)
		msg, _ := data["message"].(string)
		if _, err := exec("jobs", ref, `INSERT INTO jobs(kind,mode,status,params,counters,progress,message,started_at,finished_at,legacy_ref) VALUES('inbound_import',?,?,?,?,100,?,?,?,?)`,
			map[bool]string{true: "demo", false: "live"}[data["demo"] == true], status, js(map[string]any{"legacy": true}), jsonText(j["data"], "{}"), msg, str(j["created_at"]), str(j["created_at"]), ref); err != nil {
			return rep, err
		}
	}
	for _, e := range tables["events"] {
		ref := fmt.Sprintf("py:events:%d", num(e["id"]))
		var job any
		if rid := num(e["run_id"]); rid > 0 {
			if id, ok := runMap[rid]; ok && id > 0 {
				job = id
			}
		}
		if _, err := exec("events", ref, `INSERT INTO events(job_id,created_at,kind,message,detail,legacy_ref) VALUES(?,?,?,?,?,?)`, job, str(e["created_at"]), str(e["kind"]), str(e["message"]), jsonText(e["detail"], "{}"), ref); err != nil {
			return rep, err
		}
	}
	for _, c := range tables["companies"] {
		ref := fmt.Sprintf("py:companies:%d", num(c["id"]))
		var row any
		if v := num(c["sheet_row"]); v > 0 {
			row = v
		}
		demo := 0
		if mode == "demo" {
			demo = 1
		}
		created := str(c["created_at"])
		if _, err := exec("companies", ref, `INSERT INTO companies(name,name_key,domain,linkedin_key,data,partner,created_at,updated_at,sync_status,sheet_row,sheet_range,sync_error,demo,legacy_ref) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			str(c["name"]), str(c["name_key"]), nullStr(str(c["domain"])), nullStr(str(c["linkedin_key"])), jsonText(c["data"], "{}"), str(c["partner"]), created, created, str(c["sync_status"]), row, nullStr(str(c["sheet_range"])), nullStr(str(c["sync_error"])), demo, ref); err != nil {
			return rep, err
		}
	}
	for _, p := range tables["pitches"] {
		ref := fmt.Sprintf("py:pitches:%d", num(p["id"]))
		key := str(p["message_id"])
		account, _, _ := strings.Cut(key, ":")
		var d map[string]any
		_ = json.Unmarshal([]byte(jsonText(p["data"], "{}")), &d)
		name := str(p["name"])
		website, _ := d["website"].(string)
		if mode == "demo" {
			d["demo"] = true
		}
		if _, err := exec("pitches", ref, `INSERT INTO pitches(message_id,account,mode,name,name_key,domain,status,data,created_at,updated_at,received_at,error,legacy_ref) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			key, account, str(p["mode"]), name, domain.NameKey(name), domain.IdentityDomain(website), str(p["status"]), js(d), str(p["created_at"]), str(p["created_at"]), str(p["created_at"]), str(p["error"]), ref); err != nil {
			return rep, err
		}
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	// Verify: every source row is represented by legacy_ref in the target.
	for t, n := range rep.SourceCounts {
		target := map[string]string{"runs": "jobs", "inbox_jobs": "jobs"}[t]
		if target == "" {
			target = t
		}
		var got int
		_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+target+` WHERE legacy_ref LIKE ?`, "py:"+t+":%").Scan(&got)
		status := "ok"
		if got+countSkipped(rep.Skipped, t) != n {
			status = "MISMATCH"
		}
		rep.Checks = append(rep.Checks, fmt.Sprintf("%s: source %d, imported/present %d, skipped %d — %s", t, n, got, countSkipped(rep.Skipped, t), status))
	}
	after := hashSet(src)
	// Compare the database and any pre-existing WAL byte for byte. SQLite may
	// create an empty -wal/-shm sidecar when a WAL database is opened
	// read-only; that is reported but contains no data changes.
	rep.SourceUnchanged = true
	for k, v := range rep.SourceSHA256 {
		if after[k] != v {
			rep.SourceUnchanged = false
		}
	}
	for k := range after {
		if _, ok := rep.SourceSHA256[k]; !ok {
			if st, err := os.Stat(k); err == nil && st.Size() == 0 {
				rep.Checks = append(rep.Checks, "SQLite created an empty sidecar "+k+" while reading; source data unchanged")
			} else {
				rep.SourceUnchanged = false
			}
		}
	}
	_ = s.SetSetting(ctx, "migration.python.last", js(map[string]any{"source": src, "at": Now(), "alembic": rep.AlembicVersion, "imported": rep.Imported}))
	s.Audit(ctx, 0, "migration", fmt.Sprintf("Imported Python database (alembic %s): %v", rep.AlembicVersion, rep.Imported), rep.Imported)
	rep.FinishedAt = Now()
	return rep, nil
}

func countSkipped(skipped []string, table string) int {
	n := 0
	for _, s := range skipped {
		if strings.Contains(s, " py:"+table+":") {
			n++
		}
	}
	return n
}

func rowsAsMapsTx(ctx context.Context, tx *sql.Tx, table string) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, `SELECT * FROM "`+table+`" ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
