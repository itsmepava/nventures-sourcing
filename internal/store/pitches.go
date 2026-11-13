package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Pitch is an inbound submission keyed by account + Gmail message ID.
type Pitch struct {
	ID          int64           `json:"id"`
	MessageID   string          `json:"message_id"`
	Account     string          `json:"account"`
	ThreadID    string          `json:"thread_id"`
	Mode        string          `json:"mode"`
	Name        string          `json:"name"`
	NameKey     string          `json:"name_key"`
	Domain      string          `json:"domain"`
	Status      string          `json:"status"`
	Data        json.RawMessage `json:"data"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	ReceivedAt  string          `json:"received_at"`
	Error       string          `json:"error"`
	SheetURL    string          `json:"sheet_url"`
	Starred     bool            `json:"starred"`
	DuplicateOf *int64          `json:"duplicate_of"`
	AssigneeID  *int64          `json:"assignee_id"`
	Stage       string          `json:"stage"`
	FollowUpAt  string          `json:"follow_up_at"`
	JobID       *int64          `json:"job_id"`
	LegacyRef   string          `json:"legacy_ref,omitempty"`
}

const pitchCols = `id,message_id,account,thread_id,mode,name,name_key,domain,status,data,created_at,updated_at,received_at,error,sheet_url,starred,duplicate_of,job_id,COALESCE(legacy_ref,''),assignee_id,stage,follow_up_at`

func scanPitch(sc interface{ Scan(...any) error }) (Pitch, error) {
	var p Pitch
	var data string
	var star int
	var dup, job, asg sql.NullInt64
	err := sc.Scan(&p.ID, &p.MessageID, &p.Account, &p.ThreadID, &p.Mode, &p.Name, &p.NameKey, &p.Domain, &p.Status, &data, &p.CreatedAt, &p.UpdatedAt, &p.ReceivedAt, &p.Error, &p.SheetURL, &star, &dup, &job, &p.LegacyRef, &asg, &p.Stage, &p.FollowUpAt)
	if asg.Valid {
		p.AssigneeID = &asg.Int64
	}
	p.Data = json.RawMessage(data)
	p.Starred = star == 1
	if dup.Valid {
		p.DuplicateOf = &dup.Int64
	}
	if job.Valid {
		p.JobID = &job.Int64
	}
	return p, err
}

// PitchByMessage returns the pitch for a message key, if any.
func (s *Store) PitchByMessage(ctx context.Context, key string) (Pitch, error) {
	p, err := scanPitch(s.DB.QueryRowContext(ctx, `SELECT `+pitchCols+` FROM pitches WHERE message_id=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Pitch fetches one pitch.
func (s *Store) Pitch(ctx context.Context, id int64) (Pitch, error) {
	p, err := scanPitch(s.DB.QueryRowContext(ctx, `SELECT `+pitchCols+` FROM pitches WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// InsertPitch stores a pitch; the unique message key prevents repeats.
func (s *Store) InsertPitch(ctx context.Context, p Pitch) (Pitch, error) {
	now := Now()
	var dup, job any
	if p.DuplicateOf != nil {
		dup = *p.DuplicateOf
	}
	if p.JobID != nil {
		job = *p.JobID
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO pitches(message_id,account,thread_id,mode,name,name_key,domain,status,data,created_at,updated_at,received_at,error,duplicate_of,job_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.MessageID, p.Account, p.ThreadID, p.Mode, p.Name, p.NameKey, p.Domain, p.Status, string(p.Data), now, now, p.ReceivedAt, p.Error, dup, job)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Pitch{}, ErrDuplicate
		}
		return Pitch{}, err
	}
	id, _ := res.LastInsertId()
	return s.Pitch(ctx, id)
}

// UpdatePitch persists edits and status.
func (s *Store) UpdatePitch(ctx context.Context, p Pitch) error {
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE pitches SET name=?, name_key=?, domain=?, status=?, data=?, error=?, updated_at=? WHERE id=?`, p.Name, p.NameKey, p.Domain, p.Status, string(p.Data), p.Error, Now(), p.ID)
	return err
}

// SetPitchStatus updates status/error only.
func (s *Store) SetPitchStatus(ctx context.Context, id int64, status, errMsg string) error {
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE pitches SET status=?, error=?, updated_at=? WHERE id=?`, status, errMsg, Now(), id)
	return err
}

// FindPitchIdentity returns an earlier pitch for the same company, if any.
// Only company name and a non-shared website domain are identities; a shared
// founder email or document host never merges businesses.
func (s *Store) FindPitchIdentity(ctx context.Context, nameKey, domain string, excludeID int64) (int64, bool) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM pitches WHERE id<>? AND status NOT IN ('skipped','duplicate') AND ((name_key<>'' AND name_key=?) OR (domain<>'' AND domain=?)) ORDER BY id LIMIT 1`, excludeID, nameKey, domain).Scan(&id)
	return id, err == nil
}

// ThreadHasPitch reports whether a thread already has a non-skipped pitch.
func (s *Store) ThreadHasPitch(ctx context.Context, threadID string, excludeID int64) (int64, bool) {
	if threadID == "" {
		return 0, false
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM pitches WHERE thread_id=? AND id<>? AND status NOT IN ('skipped') ORDER BY id LIMIT 1`, threadID, excludeID).Scan(&id)
	return id, err == nil
}

// Pitches lists pitches.
func (s *Store) Pitches(ctx context.Context, lq ListQuery) ([]Pitch, int, error) {
	lq.norm()
	where := []string{"1=1"}
	var args []any
	if lq.Q != "" {
		l := likeEscape(lq.Q)
		where = append(where, `(lower(name) LIKE ? ESCAPE '\' OR lower(json_extract(data,'$.description')) LIKE ? ESCAPE '\' OR lower(json_extract(data,'$.contact')) LIKE ? ESCAPE '\')`)
		args = append(args, l, l, l)
	}
	switch lq.Status {
	case "":
	case "synced":
		where = append(where, `status IN ('verified','already_exists','demo_verified')`)
	case "attention":
		where = append(where, `status IN ('review','error','conflict','uncertain')`)
	default:
		where = append(where, `status=?`)
		args = append(args, lq.Status)
	}
	if lq.Starred {
		where = append(where, `starred=1`)
	}
	where, args = lq.workflowFilters(where, args)
	if len(lq.IDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(lq.IDs)), ",")
		where = append(where, `id IN (`+ph+`)`)
		for _, id := range lq.IDs {
			args = append(args, id)
		}
	}
	order := map[string]string{"name": "lower(name)", "received": "received_at", "status": "status", "created": "id", "follow_up": "CASE WHEN follow_up_at='' THEN '9999' ELSE follow_up_at END"}[lq.Sort]
	if order == "" {
		order, lq.Desc = "received_at", true
	}
	dir := "ASC"
	if lq.Desc {
		dir = "DESC"
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pitches WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+pitchCols+` FROM pitches WHERE `+w+` ORDER BY `+order+` `+dir+`, id DESC LIMIT ? OFFSET ?`, append(args, lq.PerPage, (lq.Page-1)*lq.PerPage)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Pitch{}
	for rows.Next() {
		p, err := scanPitch(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// PitchCounts returns counts by status.
func (s *Store) PitchCounts(ctx context.Context) map[string]int {
	out := map[string]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT status, COUNT(*) FROM pitches GROUP BY status`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if rows.Scan(&k, &n) == nil {
			out[k] = n
		}
	}
	return out
}

// ---- sync operations ----

// SyncOp is a durable record of one intended sheet write. It is persisted
// before any network write so that an interrupted or uncertain append is
// reconciled by reading the sheet, never blindly repeated.
type SyncOp struct {
	ID          int64    `json:"id"`
	Target      string   `json:"target"` // pitch | company
	EntityID    int64    `json:"entity_id"`
	Destination string   `json:"destination"`
	Payload     []string `json:"payload"`
	PayloadHash string   `json:"payload_hash"`
	Status      string   `json:"status"` // pending, writing, uncertain, verified, already_exists, conflict, failed
	SheetRow    *int64   `json:"sheet_row"`
	SheetRange  string   `json:"sheet_range"`
	Error       string   `json:"error"`
	Attempts    int      `json:"attempts"`
	JobID       *int64   `json:"job_id"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// HashRow returns a stable hash of a sheet row.
func HashRow(row []string) string {
	b, _ := json.Marshal(row)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

const opCols = `id,target,entity_id,destination,payload,payload_hash,status,sheet_row,COALESCE(sheet_range,''),error,attempts,job_id,created_at,updated_at`

func scanOp(sc interface{ Scan(...any) error }) (SyncOp, error) {
	var o SyncOp
	var payload string
	var row, job sql.NullInt64
	err := sc.Scan(&o.ID, &o.Target, &o.EntityID, &o.Destination, &payload, &o.PayloadHash, &o.Status, &row, &o.SheetRange, &o.Error, &o.Attempts, &job, &o.CreatedAt, &o.UpdatedAt)
	_ = json.Unmarshal([]byte(payload), &o.Payload)
	if row.Valid {
		o.SheetRow = &row.Int64
	}
	if job.Valid {
		o.JobID = &job.Int64
	}
	return o, err
}

// BeginSyncOp persists a pending write intent.
func (s *Store) BeginSyncOp(ctx context.Context, target string, entity int64, dest string, payload []string, jobID int64) (SyncOp, error) {
	now := Now()
	var job any
	if jobID > 0 {
		job = jobID
	}
	res, err := s.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO sync_ops(target,entity_id,destination,payload,payload_hash,status,attempts,job_id,created_at,updated_at) VALUES(?,?,?,?,?,'pending',0,?,?,?)`, target, entity, dest, js(payload), HashRow(payload), job, now, now)
	if err != nil {
		return SyncOp{}, err
	}
	id, _ := res.LastInsertId()
	return scanOp(s.DB.QueryRowContext(ctx, `SELECT `+opCols+` FROM sync_ops WHERE id=?`, id))
}

// MarkSyncOp updates an operation's state.
func (s *Store) MarkSyncOp(ctx context.Context, id int64, status string, row int64, rng, errMsg string, attempt bool) error {
	var r any
	if row > 0 {
		r = row
	}
	inc := 0
	if attempt {
		inc = 1
	}
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE sync_ops SET status=?, sheet_row=COALESCE(?, sheet_row), sheet_range=COALESCE(?, sheet_range), error=?, attempts=attempts+?, updated_at=? WHERE id=?`, status, r, nullStr(rng), errMsg, inc, Now(), id)
	return err
}

// LatestSyncOp returns the most recent op for an entity.
func (s *Store) LatestSyncOp(ctx context.Context, target string, entity int64) (SyncOp, bool) {
	o, err := scanOp(s.DB.QueryRowContext(ctx, `SELECT `+opCols+` FROM sync_ops WHERE target=? AND entity_id=? ORDER BY id DESC LIMIT 1`, target, entity))
	return o, err == nil
}

// SyncOps lists operations for an entity or status.
func (s *Store) SyncOps(ctx context.Context, target string, entity int64, limit int) ([]SyncOp, error) {
	q := `SELECT ` + opCols + ` FROM sync_ops WHERE 1=1`
	var args []any
	if target != "" {
		q += ` AND target=?`
		args = append(args, target)
	}
	if entity > 0 {
		q += ` AND entity_id=?`
		args = append(args, entity)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SyncOp{}
	for rows.Next() {
		o, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// RecoverSyncOps marks writes interrupted by a restart as uncertain.
func (s *Store) RecoverSyncOps(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE sync_ops SET status='uncertain', error='Interrupted during write; the sheet will be read and reconciled before any retry', updated_at=? WHERE status IN ('pending','writing')`, Now())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, _ = s.DB.ExecContext(ctx, `UPDATE pitches SET status='uncertain', error='Sheet write interrupted; retry will reconcile' WHERE id IN (SELECT entity_id FROM sync_ops WHERE target='pitch' AND status='uncertain') AND status='syncing'`)
		_, _ = s.DB.ExecContext(ctx, `UPDATE companies SET sync_status='uncertain', sync_error='Sheet write interrupted; retry will reconcile' WHERE id IN (SELECT entity_id FROM sync_ops WHERE target='company' AND status='uncertain') AND sync_status='syncing'`)
	}
	return n, nil
}
