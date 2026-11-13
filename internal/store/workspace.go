package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---- actor (who is doing something) ----

// Actor identifies the person (or the scheduler) behind an action.
type Actor struct {
	ID   int64
	Name string
}

type actorKey struct{}

// WithActor attaches an actor to a context so audit events record it.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the context's actor (zero if none).
func ActorFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}

// ---- users ----

// User is a workspace member. The password hash never leaves the store.
type User struct {
	ID                 int64  `json:"id"`
	Email              string `json:"email"`
	Name               string `json:"name"`
	Role               string `json:"role"`
	Active             bool   `json:"active"`
	MustChangePassword bool   `json:"must_change_password"`
	CreatedAt          string `json:"created_at"`
	LastLoginAt        string `json:"last_login_at"`
	hash               string
}

// PasswordHash returns the stored hash (for verification only).
func (u User) PasswordHash() string { return u.hash }

const userCols = `id,email,name,role,password_hash,active,must_change_password,created_at,last_login_at`

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var u User
	var active, must int
	err := sc.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.hash, &active, &must, &u.CreatedAt, &u.LastLoginAt)
	u.Active, u.MustChangePassword = active == 1, must == 1
	return u, err
}

// ErrEmailTaken is returned when an email already has an account.
var ErrEmailTaken = errors.New("an account with this email already exists")

// CountUsers returns the number of accounts.
func (s *Store) CountUsers(ctx context.Context) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n
}

// CreateUser adds an account.
func (s *Store) CreateUser(ctx context.Context, email, name, role, hash string, mustChange bool) (User, error) {
	must := 0
	if mustChange {
		must = 1
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO users(email,name,role,password_hash,must_change_password,created_at) VALUES(?,?,?,?,?,?)`, strings.TrimSpace(email), strings.TrimSpace(name), role, hash, must, Now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return s.User(ctx, id)
}

// CreateFirstAdmin creates the first account only if none exists, atomically.
func (s *Store) CreateFirstAdmin(ctx context.Context, email, name, hash string) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, errors.New("setup is already complete")
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,name,role,password_hash,created_at) VALUES(?,?,'admin',?,?)`, strings.TrimSpace(email), strings.TrimSpace(name), hash, Now())
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return s.User(ctx, id)
}

// User fetches an account by ID.
func (s *Store) User(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// UserByEmail fetches an account by email (case-insensitive).
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email=?`, strings.TrimSpace(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// Users lists all accounts.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY active DESC, lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ErrLastAdmin protects against locking everyone out.
var ErrLastAdmin = errors.New("the workspace needs at least one active admin")

// UpdateUser changes name, role or active state. It refuses to remove the
// last active admin, inside one transaction.
func (s *Store) UpdateUser(ctx context.Context, id int64, name *string, role *string, active *bool) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if name != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET name=? WHERE id=?`, strings.TrimSpace(*name), id); err != nil {
			return User{}, err
		}
	}
	if role != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET role=? WHERE id=?`, *role, id); err != nil {
			return User{}, err
		}
	}
	if active != nil {
		v := 0
		if *active {
			v = 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET active=? WHERE id=?`, v, id); err != nil {
			return User{}, err
		}
		if !*active {
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id); err != nil {
				return User{}, err
			}
		}
	}
	var admins int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND active=1`).Scan(&admins); err != nil {
		return User{}, err
	}
	if admins == 0 {
		return User{}, ErrLastAdmin
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return s.User(ctx, id)
}

// SetPassword replaces a password hash and ends other sessions.
func (s *Store) SetPassword(ctx context.Context, id int64, hash string, mustChange bool, keepSession string) error {
	must := 0
	if mustChange {
		must = 1
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE users SET password_hash=?, must_change_password=? WHERE id=?`, hash, must, id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, id, keepSession)
	return err
}

// ---- sessions ----

// CreateSession stores a hashed session token.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,created_at,expires_at,last_seen_at) VALUES(?,?,?,?,?)`, tokenHash, userID, now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err == nil {
		_, _ = s.DB.ExecContext(ctx, `UPDATE users SET last_login_at=? WHERE id=?`, now.Format(time.RFC3339Nano), userID)
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<?`, now.Format(time.RFC3339Nano))
	}
	return err
}

// SessionUser returns the active user for a session, extending it (sliding
// expiry) at most once a minute.
func (s *Store) SessionUser(ctx context.Context, tokenHash string, ttl time.Duration) (User, error) {
	var uid int64
	var exp, seen string
	err := s.DB.QueryRowContext(ctx, `SELECT user_id, expires_at, last_seen_at FROM sessions WHERE token_hash=?`, tokenHash).Scan(&uid, &exp, &seen)
	if err != nil {
		return User{}, ErrNotFound
	}
	e, _ := time.Parse(time.RFC3339Nano, exp)
	if time.Now().After(e) {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
		return User{}, ErrNotFound
	}
	u, err := s.User(ctx, uid)
	if err != nil || !u.Active {
		return User{}, ErrNotFound
	}
	if t, _ := time.Parse(time.RFC3339Nano, seen); time.Since(t) > time.Minute {
		now := time.Now().UTC()
		_, _ = s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?, expires_at=? WHERE token_hash=?`, now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano), tokenHash)
	}
	return u, nil
}

// DeleteSession ends a session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
}

// ---- workflow on pitches and companies ----

// Workflow is the team-tracking state of a pitch or company. It is local to
// nVentures and never written to the sheets.
type Workflow struct {
	AssigneeID *int64 `json:"assignee_id"`
	Stage      string `json:"stage"`
	FollowUpAt string `json:"follow_up_at"`
}

// PitchStages and CompanyStages are the allowed pipeline stages.
var (
	PitchStages   = []string{"new", "reviewing", "contacted", "meeting", "passed", "progressed"}
	CompanyStages = []string{"not_contacted", "contacted", "meeting", "passed", "progressed"}
)

func validStage(stages []string, s string) bool {
	for _, x := range stages {
		if x == s {
			return true
		}
	}
	return false
}

// SetWorkflow updates assignment, stage and follow-up date.
func (s *Store) SetWorkflow(ctx context.Context, table string, id int64, w Workflow) error {
	stages := PitchStages
	if table == "companies" {
		stages = CompanyStages
	} else if table != "pitches" {
		return fmt.Errorf("bad table")
	}
	if !validStage(stages, w.Stage) {
		return fmt.Errorf("unknown stage %q", w.Stage)
	}
	if w.FollowUpAt != "" {
		if _, err := time.Parse("2006-01-02", w.FollowUpAt); err != nil {
			return fmt.Errorf("follow-up date must be YYYY-MM-DD")
		}
	}
	var assignee any
	if w.AssigneeID != nil && *w.AssigneeID > 0 {
		u, err := s.User(ctx, *w.AssigneeID)
		if err != nil || !u.Active {
			return fmt.Errorf("assignee is not an active team member")
		}
		assignee = *w.AssigneeID
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE `+table+` SET assignee_id=?, stage=?, follow_up_at=? WHERE id=?`, assignee, w.Stage, w.FollowUpAt, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- notes ----

// Note is an internal comment on a pitch or company.
type Note struct {
	ID       int64  `json:"id"`
	Entity   string `json:"entity"`
	EntityID int64  `json:"entity_id"`
	UserID   *int64 `json:"user_id"`
	Author   string `json:"author"`
	Body     string `json:"body"`
	Created  string `json:"created_at"`
}

// AddNote stores a note by the context's actor.
func (s *Store) AddNote(ctx context.Context, entity string, entityID int64, body string) (Note, error) {
	a := ActorFrom(ctx)
	var uid any
	if a.ID > 0 {
		uid = a.ID
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO notes(entity,entity_id,user_id,author,body,created_at) VALUES(?,?,?,?,?,?)`, entity, entityID, uid, a.Name, body, now)
	if err != nil {
		return Note{}, err
	}
	id, _ := res.LastInsertId()
	n := Note{ID: id, Entity: entity, EntityID: entityID, Author: a.Name, Body: body, Created: now}
	if a.ID > 0 {
		n.UserID = &a.ID
	}
	return n, nil
}

// Notes lists notes on a record, oldest first.
func (s *Store) Notes(ctx context.Context, entity string, entityID int64) ([]Note, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,entity,entity_id,user_id,author,body,created_at FROM notes WHERE entity=? AND entity_id=? ORDER BY id`, entity, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		var uid sql.NullInt64
		if err := rows.Scan(&n.ID, &n.Entity, &n.EntityID, &uid, &n.Author, &n.Body, &n.Created); err != nil {
			return nil, err
		}
		if uid.Valid {
			n.UserID = &uid.Int64
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---- reply templates ----

// Template is a reply draft used to compose an email in the user's own mail program.
type Template struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Updated string `json:"updated_at"`
}

// Templates lists reply templates.
func (s *Store) Templates(ctx context.Context) ([]Template, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,subject,body,updated_at FROM reply_templates ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.ID, &t.Name, &t.Subject, &t.Body, &t.Updated); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveTemplate creates (id 0) or updates a template.
func (s *Store) SaveTemplate(ctx context.Context, t Template) (Template, error) {
	var err error
	if t.ID == 0 {
		var res sql.Result
		res, err = s.DB.ExecContext(ctx, `INSERT INTO reply_templates(name,subject,body,updated_at) VALUES(?,?,?,?)`, t.Name, t.Subject, t.Body, Now())
		if err == nil {
			t.ID, _ = res.LastInsertId()
		}
	} else {
		var res sql.Result
		res, err = s.DB.ExecContext(ctx, `UPDATE reply_templates SET name=?, subject=?, body=?, updated_at=? WHERE id=?`, t.Name, t.Subject, t.Body, Now(), t.ID)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return t, ErrNotFound
			}
		}
	}
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return t, errors.New("a template with this name already exists")
	}
	return t, err
}

// DeleteTemplate removes a template.
func (s *Store) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM reply_templates WHERE id=?`, id)
	return err
}

// ---- schedules ----

// Schedule runs a job kind periodically.
type Schedule struct {
	ID           int64          `json:"id"`
	Kind         string         `json:"kind"`
	Params       map[string]any `json:"params"`
	EveryMinutes int            `json:"every_minutes"`
	NextRunAt    string         `json:"next_run_at"`
	Enabled      bool           `json:"enabled"`
	LastJobID    *int64         `json:"last_job_id"`
	LastStatus   string         `json:"last_status"`
	CreatedBy    *int64         `json:"created_by"`
	CreatedAt    string         `json:"created_at"`
}

const schedCols = `id,kind,params,every_minutes,next_run_at,enabled,last_job_id,last_status,created_by,created_at`

func scanSched(sc interface{ Scan(...any) error }) (Schedule, error) {
	var x Schedule
	var p string
	var en int
	var last, by sql.NullInt64
	err := sc.Scan(&x.ID, &x.Kind, &p, &x.EveryMinutes, &x.NextRunAt, &en, &last, &x.LastStatus, &by, &x.CreatedAt)
	_ = json.Unmarshal([]byte(p), &x.Params)
	if x.Params == nil {
		x.Params = map[string]any{}
	}
	x.Enabled = en == 1
	if last.Valid {
		x.LastJobID = &last.Int64
	}
	if by.Valid {
		x.CreatedBy = &by.Int64
	}
	return x, err
}

// Schedules lists schedules.
func (s *Store) Schedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+schedCols+` FROM schedules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		x, err := scanSched(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SaveSchedule creates (ID 0) or updates a schedule.
func (s *Store) SaveSchedule(ctx context.Context, x Schedule) (Schedule, error) {
	en := 0
	if x.Enabled {
		en = 1
	}
	if x.ID == 0 {
		var by any
		if a := ActorFrom(ctx); a.ID > 0 {
			by = a.ID
		}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO schedules(kind,params,every_minutes,next_run_at,enabled,created_by,created_at) VALUES(?,?,?,?,?,?,?)`, x.Kind, js(x.Params), x.EveryMinutes, x.NextRunAt, en, by, Now())
		if err != nil {
			return x, err
		}
		x.ID, _ = res.LastInsertId()
	} else if _, err := s.DB.ExecContext(ctx, `UPDATE schedules SET params=?, every_minutes=?, next_run_at=?, enabled=? WHERE id=?`, js(x.Params), x.EveryMinutes, x.NextRunAt, en, x.ID); err != nil {
		return x, err
	}
	return scanSched(s.DB.QueryRowContext(ctx, `SELECT `+schedCols+` FROM schedules WHERE id=?`, x.ID))
}

// Schedule fetches one schedule.
func (s *Store) Schedule(ctx context.Context, id int64) (Schedule, error) {
	x, err := scanSched(s.DB.QueryRowContext(ctx, `SELECT `+schedCols+` FROM schedules WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// DeleteSchedule removes a schedule.
func (s *Store) DeleteSchedule(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM schedules WHERE id=?`, id)
	return err
}

// DueSchedules returns enabled schedules whose next run is due.
func (s *Store) DueSchedules(ctx context.Context, now time.Time) ([]Schedule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+schedCols+` FROM schedules WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		x, err := scanSched(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// MarkScheduleRun records a run (or a skipped attempt) and the next time.
func (s *Store) MarkScheduleRun(ctx context.Context, id int64, jobID int64, status string, next time.Time) error {
	var j any
	if jobID > 0 {
		j = jobID
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE schedules SET last_job_id=COALESCE(?, last_job_id), last_status=?, next_run_at=? WHERE id=?`, j, status, next.UTC().Format(time.RFC3339Nano), id)
	return err
}

// ---- notifications ----

// Notification is an in-app message for one user.
type Notification struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Link    string `json:"link"`
	Created string `json:"created_at"`
	ReadAt  string `json:"read_at"`
}

// Notify sends a notification to each active user with one of the roles
// (or to explicit user IDs), skipping the actor.
func (s *Store) Notify(ctx context.Context, userIDs []int64, roles []string, kind, msg, link string) {
	targets := map[int64]bool{}
	for _, id := range userIDs {
		targets[id] = true
	}
	if len(roles) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(roles)), ",")
		args := make([]any, len(roles))
		for i, r := range roles {
			args[i] = r
		}
		rows, err := s.DB.QueryContext(ctx, `SELECT id FROM users WHERE active=1 AND role IN (`+ph+`)`, args...)
		if err == nil {
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					targets[id] = true
				}
			}
			rows.Close()
		}
	}
	delete(targets, ActorFrom(ctx).ID)
	for id := range targets {
		_, _ = s.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO notifications(user_id,kind,message,link,created_at) VALUES(?,?,?,?,?)`, id, kind, msg, link, Now())
	}
}

// Notifications returns a user's recent notifications and unread count.
func (s *Store) Notifications(ctx context.Context, userID int64, limit int) ([]Notification, int, error) {
	var unread int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at=''`, userID).Scan(&unread)
	rows, err := s.DB.QueryContext(ctx, `SELECT id,kind,message,link,created_at,read_at FROM notifications WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Kind, &n.Message, &n.Link, &n.Created, &n.ReadAt); err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, unread, rows.Err()
}

// MarkNotificationsRead marks the given (or all) notifications read.
func (s *Store) MarkNotificationsRead(ctx context.Context, userID int64, ids []int64) error {
	if len(ids) == 0 {
		_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND read_at=''`, Now(), userID)
		return err
	}
	for _, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND id=? AND read_at=''`, Now(), userID, id); err != nil {
			return err
		}
	}
	return nil
}

// ---- my queue ----

// QueueItem is an assigned pitch or company.
type QueueItem struct {
	Entity     string `json:"entity"`
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Stage      string `json:"stage"`
	Status     string `json:"status"`
	FollowUpAt string `json:"follow_up_at"`
	Updated    string `json:"updated_at"`
}

// Queue returns records assigned to a user, follow-ups first.
func (s *Store) Queue(ctx context.Context, userID int64) ([]QueueItem, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT entity, id, name, stage, status, fu, up FROM (
		SELECT 'pitch' AS entity, id, name, stage, status, follow_up_at AS fu, updated_at AS up FROM pitches WHERE assignee_id=? AND stage NOT IN ('passed','progressed')
		UNION ALL
		SELECT 'company', id, name, stage, sync_status, follow_up_at, updated_at FROM companies WHERE assignee_id=? AND stage NOT IN ('passed','progressed'))
		ORDER BY (fu=''), fu, up DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueItem{}
	for rows.Next() {
		var q QueueItem
		if err := rows.Scan(&q.Entity, &q.ID, &q.Name, &q.Stage, &q.Status, &q.FollowUpAt, &q.Updated); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
