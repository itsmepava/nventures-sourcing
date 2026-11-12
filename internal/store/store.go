// Package store owns the SQLite database: versioned migrations, durable job
// state, leases and every query. It uses database/sql with the CGO-free
// modernc.org/sqlite driver.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store wraps the database.
type Store struct {
	DB   *sql.DB
	Path string
}

// Now returns the canonical timestamp format.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Open opens (creating if needed) a SQLite database and applies migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, p := range []string{"foreign_keys(1)", "busy_timeout(10000)", "journal_mode(WAL)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	s := &Store{DB: db, Path: path}
	if err := s.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Migrate applies pending embedded migrations atomically, in order.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		base := filepath.Base(f)
		v, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %s", base)
		}
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=?`, v).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, _ := migrationFS.ReadFile(f)
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", base, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, v, base, Now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration.
func (s *Store) SchemaVersion(ctx context.Context) int {
	var v sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64)
}

// Backup writes a transactionally consistent copy using VACUUM INTO.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination exists: %s", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0o600)
}

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("not found")

// ---- settings ----

// GetSetting returns a setting or def.
func (s *Store) GetSetting(ctx context.Context, key, def string) string {
	var v string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, Now())
	return err
}

// Settings returns all settings with a prefix.
func (s *Store) Settings(ctx context.Context, prefix string) map[string]string {
	out := map[string]string{}
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings WHERE key LIKE ?`, prefix+"%")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

// ---- leases ----

// AcquireLease takes a named lease if free or expired. The check and write
// happen in one IMMEDIATE transaction so concurrent starts cannot both win.
func (s *Store) AcquireLease(ctx context.Context, name, owner string, jobID int64, ttl time.Duration) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var cur string
	var exp string
	err = tx.QueryRowContext(ctx, `SELECT owner, expires_at FROM leases WHERE name=?`, name).Scan(&cur, &exp)
	if err == nil {
		t, _ := time.Parse(time.RFC3339Nano, exp)
		if time.Now().Before(t) && cur != owner {
			return false, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	expires := time.Now().Add(ttl).UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO leases(name,owner,job_id,expires_at) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET owner=excluded.owner, job_id=excluded.job_id, expires_at=excluded.expires_at`, name, owner, jobID, expires); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// RenewLease extends a lease held by owner.
func (s *Store) RenewLease(ctx context.Context, name, owner string, ttl time.Duration) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE leases SET expires_at=? WHERE name=? AND owner=?`, time.Now().Add(ttl).UTC().Format(time.RFC3339Nano), name, owner)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("lease %s lost", name)
	}
	return nil
}

// ReleaseLease drops a lease held by owner.
func (s *Store) ReleaseLease(ctx context.Context, name, owner string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM leases WHERE name=? AND owner=?`, name, owner)
	return err
}

// LeaseHolder reports the job holding a lease, if unexpired.
func (s *Store) LeaseHolder(ctx context.Context, name string) (int64, bool) {
	var id sql.NullInt64
	var exp string
	if err := s.DB.QueryRowContext(ctx, `SELECT job_id, expires_at FROM leases WHERE name=?`, name).Scan(&id, &exp); err != nil {
		return 0, false
	}
	t, _ := time.Parse(time.RFC3339Nano, exp)
	return id.Int64, time.Now().Before(t)
}

// ClearLeasesExcept removes job leases not owned by owner (startup recovery).
func (s *Store) ClearLeasesExcept(ctx context.Context, keep string, owner string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM leases WHERE name<>? AND owner<>?`, keep, owner)
	return err
}

// ---- events ----

// Event is an audit entry.
type Event struct {
	ID        int64           `json:"id"`
	JobID     *int64          `json:"job_id"`
	CreatedAt string          `json:"created_at"`
	Kind      string          `json:"kind"`
	Message   string          `json:"message"`
	Detail    json.RawMessage `json:"detail"`
}

// Audit appends an event.
func (s *Store) Audit(ctx context.Context, jobID int64, kind, message string, detail any) {
	if detail == nil {
		detail = map[string]any{}
	}
	var jid any
	if jobID > 0 {
		jid = jobID
	}
	_, _ = s.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO events(job_id,created_at,kind,message,detail) VALUES(?,?,?,?,?)`, jid, Now(), kind, message, js(detail))
}

// Events lists events, newest first.
func (s *Store) Events(ctx context.Context, jobID int64, kind string, after int64, limit int) ([]Event, error) {
	q := `SELECT id, job_id, created_at, kind, message, detail FROM events WHERE id>?`
	args := []any{after}
	if jobID > 0 {
		q += ` AND job_id=?`
		args = append(args, jobID)
	}
	if kind != "" {
		q += ` AND kind=?`
		args = append(args, kind)
	}
	if jobID > 0 {
		q += ` ORDER BY id ASC LIMIT ?`
	} else {
		q += ` ORDER BY id DESC LIMIT ?`
	}
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var jid sql.NullInt64
		var d string
		if err := rows.Scan(&e.ID, &jid, &e.CreatedAt, &e.Kind, &e.Message, &d); err != nil {
			return nil, err
		}
		if jid.Valid {
			e.JobID = &jid.Int64
		}
		e.Detail = json.RawMessage(d)
		if !json.Valid(e.Detail) {
			e.Detail = json.RawMessage(`{}`)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- fetch cache ----

// CacheTable adapts the store to discovery.Cache without importing it.
type CacheRow struct {
	URL, ContentType, ETag, LastModified string
	Status                               int
	Body                                 []byte
	Truncated                            bool
	FetchedAt                            time.Time
}

// GetCache returns a cached page.
func (s *Store) GetCache(ctx context.Context, u string) (*CacheRow, error) {
	var r CacheRow
	var t string
	var tr int
	err := s.DB.QueryRowContext(ctx, `SELECT url,status,content_type,etag,last_modified,body,truncated,fetched_at FROM fetch_cache WHERE url=?`, u).Scan(&r.URL, &r.Status, &r.ContentType, &r.ETag, &r.LastModified, &r.Body, &tr, &t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Truncated = tr == 1
	r.FetchedAt, _ = time.Parse(time.RFC3339Nano, t)
	return &r, nil
}

// PutCache stores a page.
func (s *Store) PutCache(ctx context.Context, r CacheRow) error {
	tr := 0
	if r.Truncated {
		tr = 1
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO fetch_cache(url,status,content_type,etag,last_modified,body,truncated,fetched_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(url) DO UPDATE SET status=excluded.status, content_type=excluded.content_type, etag=excluded.etag, last_modified=excluded.last_modified, body=excluded.body, truncated=excluded.truncated, fetched_at=excluded.fetched_at`, r.URL, r.Status, r.ContentType, r.ETag, r.LastModified, r.Body, tr, r.FetchedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// ClearCache empties the fetch cache.
func (s *Store) ClearCache(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM fetch_cache`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
