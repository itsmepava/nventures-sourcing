package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ---- partners ----

// Partner is a portfolio partner to rotate through.
type Partner struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Website     string          `json:"website"`
	Relevant    bool            `json:"relevant"`
	LastSourced string          `json:"last_sourced"`
	RunCount    int             `json:"run_count"`
	LastAdded   int             `json:"last_added"`
	LastReport  json.RawMessage `json:"last_report"`
}

const partnerCols = `id,name,website,relevant,last_sourced,run_count,last_added,last_report`

func scanPartner(sc interface{ Scan(...any) error }) (Partner, error) {
	var p Partner
	var rel int
	var rep string
	err := sc.Scan(&p.ID, &p.Name, &p.Website, &rel, &p.LastSourced, &p.RunCount, &p.LastAdded, &rep)
	p.Relevant = rel == 1
	p.LastReport = json.RawMessage(rep)
	return p, err
}

// Partners lists partners in fair rotation order (least recently sourced first).
func (s *Store) Partners(ctx context.Context, relevantOnly bool, limit int) ([]Partner, error) {
	q := `SELECT ` + partnerCols + ` FROM partners`
	if relevantOnly {
		q += ` WHERE relevant=1`
	}
	q += ` ORDER BY last_sourced, run_count, id`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Partner{}
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddPartner inserts a partner.
func (s *Store) AddPartner(ctx context.Context, name, website string, relevant bool) (Partner, error) {
	rel := 0
	if relevant {
		rel = 1
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO partners(name,website,relevant,created_at) VALUES(?,?,?,?)`, name, website, rel, Now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Partner{}, fmt.Errorf("partner already exists")
		}
		return Partner{}, err
	}
	id, _ := res.LastInsertId()
	return s.Partner(ctx, id)
}

// Partner fetches one partner.
func (s *Store) Partner(ctx context.Context, id int64) (Partner, error) {
	p, err := scanPartner(s.DB.QueryRowContext(ctx, `SELECT `+partnerCols+` FROM partners WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// UpdatePartner changes website/relevance.
func (s *Store) UpdatePartner(ctx context.Context, id int64, website *string, relevant *bool) (Partner, error) {
	if website != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE partners SET website=? WHERE id=?`, *website, id); err != nil {
			return Partner{}, err
		}
	}
	if relevant != nil {
		v := 0
		if *relevant {
			v = 1
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE partners SET relevant=? WHERE id=?`, v, id); err != nil {
			return Partner{}, err
		}
	}
	return s.Partner(ctx, id)
}

// MergePartner imports a partner from the sheet, keeping the greater counters.
func (s *Store) MergePartner(ctx context.Context, name, website string, relevant bool, lastSourced string, runCount int) error {
	rel := 0
	if relevant {
		rel = 1
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO partners(name,website,relevant,last_sourced,run_count,created_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET website=CASE WHEN excluded.website<>'' THEN excluded.website ELSE partners.website END, relevant=excluded.relevant,
		last_sourced=MAX(partners.last_sourced, excluded.last_sourced), run_count=MAX(partners.run_count, excluded.run_count)`, name, website, rel, lastSourced, runCount, Now())
	return err
}

// MarkPartnerSourced records a completed partner visit.
func (s *Store) MarkPartnerSourced(ctx context.Context, id int64, added int, report any) error {
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE partners SET last_sourced=?, last_added=?, run_count=run_count+1, last_report=? WHERE id=?`, Now(), added, js(report), id)
	return err
}

// ---- jobs ----

// Job is a persisted background operation.
type Job struct {
	ID              int64          `json:"id"`
	Kind            string         `json:"kind"` // outbound, inbound, sync_inbound, sync_outbound, research
	Mode            string         `json:"mode"`
	Status          string         `json:"status"`
	Params          map[string]any `json:"params"`
	Counters        map[string]any `json:"counters"`
	Progress        int            `json:"progress"`
	Message         string         `json:"message"`
	CancelRequested bool           `json:"cancel_requested"`
	StartedAt       string         `json:"started_at"`
	FinishedAt      string         `json:"finished_at"`
	HeartbeatAt     string         `json:"heartbeat_at"`
	LegacyRef       string         `json:"legacy_ref,omitempty"`
	CreatedBy       *int64         `json:"created_by"`
	CreatedByName   string         `json:"created_by_name"`
}

const jobCols = `id,kind,mode,status,params,counters,progress,message,cancel_requested,started_at,COALESCE(finished_at,''),COALESCE(heartbeat_at,''),COALESCE(legacy_ref,''),created_by,created_by_name`

func scanJob(sc interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var p, c string
	var cancel int
	var by sql.NullInt64
	err := sc.Scan(&j.ID, &j.Kind, &j.Mode, &j.Status, &p, &c, &j.Progress, &j.Message, &cancel, &j.StartedAt, &j.FinishedAt, &j.HeartbeatAt, &j.LegacyRef, &by, &j.CreatedByName)
	if by.Valid {
		j.CreatedBy = &by.Int64
	}
	_ = json.Unmarshal([]byte(p), &j.Params)
	_ = json.Unmarshal([]byte(c), &j.Counters)
	if j.Params == nil {
		j.Params = map[string]any{}
	}
	if j.Counters == nil {
		j.Counters = map[string]any{}
	}
	j.CancelRequested = cancel == 1
	return j, err
}

// CreateJob inserts a running job.
func (s *Store) CreateJob(ctx context.Context, kind, mode string, params any) (Job, error) {
	now := Now()
	a := ActorFrom(ctx)
	var by any
	if a.ID > 0 {
		by = a.ID
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO jobs(kind,mode,status,params,counters,message,started_at,heartbeat_at,created_by,created_by_name) VALUES(?,?,?,?,?,?,?,?,?,?)`, kind, mode, "running", js(params), "{}", "Starting", now, now, by, a.Name)
	if err != nil {
		return Job{}, err
	}
	id, _ := res.LastInsertId()
	return s.Job(ctx, id)
}

// Job fetches one job.
func (s *Store) Job(ctx context.Context, id int64) (Job, error) {
	j, err := scanJob(s.DB.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

// Jobs lists recent jobs.
func (s *Store) Jobs(ctx context.Context, kind string, limit int) ([]Job, error) {
	q := `SELECT ` + jobCols + ` FROM jobs`
	var args []any
	if kind != "" {
		q += ` WHERE kind LIKE ?`
		args = append(args, kind+"%")
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// UpdateJob persists progress.
func (s *Store) UpdateJob(ctx context.Context, id int64, counters map[string]any, progress int, message string) error {
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE jobs SET counters=?, progress=?, message=?, heartbeat_at=? WHERE id=?`, js(counters), progress, message, Now(), id)
	return err
}

// FinishJob sets a terminal status.
func (s *Store) FinishJob(ctx context.Context, id int64, status string, counters map[string]any, message string) error {
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE jobs SET status=?, counters=?, progress=100, message=?, finished_at=?, heartbeat_at=? WHERE id=?`, status, js(counters), message, Now(), Now(), id)
	return err
}

// RequestCancel flags a running job for cancellation.
func (s *Store) RequestCancel(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE jobs SET cancel_requested=1 WHERE id=? AND status='running'`, id)
	return err
}

// CancelRequested reports whether cancellation was requested.
func (s *Store) CancelRequested(ctx context.Context, id int64) bool {
	var v int
	_ = s.DB.QueryRowContext(context.WithoutCancel(ctx), `SELECT cancel_requested FROM jobs WHERE id=?`, id).Scan(&v)
	return v == 1
}

// InterruptRunning marks jobs left running by a previous process.
func (s *Store) InterruptRunning(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE jobs SET status='interrupted', finished_at=?, message=message || ' · Interrupted: the app stopped before this job finished. Saved work was kept; start again to continue.' WHERE status IN ('running','queued')`, Now())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ---- companies ----

// Company is an accepted outbound company.
type Company struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	NameKey     string          `json:"name_key"`
	Domain      string          `json:"domain"`
	LinkedInKey string          `json:"linkedin_key"`
	Data        json.RawMessage `json:"data"`
	Partner     string          `json:"partner"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	SyncStatus  string          `json:"sync_status"`
	SheetRow    *int64          `json:"sheet_row"`
	SheetRange  string          `json:"sheet_range"`
	SyncError   string          `json:"sync_error"`
	SheetURL    string          `json:"sheet_url"`
	Starred     bool            `json:"starred"`
	Demo        bool            `json:"demo"`
	AssigneeID  *int64          `json:"assignee_id"`
	Stage       string          `json:"stage"`
	FollowUpAt  string          `json:"follow_up_at"`
	JobID       *int64          `json:"job_id"`
	LegacyRef   string          `json:"legacy_ref,omitempty"`
}

const companyCols = `id,name,name_key,COALESCE(domain,''),COALESCE(linkedin_key,''),data,partner,created_at,updated_at,sync_status,sheet_row,COALESCE(sheet_range,''),COALESCE(sync_error,''),sheet_url,starred,demo,job_id,COALESCE(legacy_ref,''),assignee_id,stage,follow_up_at`

func scanCompany(sc interface{ Scan(...any) error }) (Company, error) {
	var c Company
	var data string
	var row, job, asg sql.NullInt64
	var star, demo int
	err := sc.Scan(&c.ID, &c.Name, &c.NameKey, &c.Domain, &c.LinkedInKey, &data, &c.Partner, &c.CreatedAt, &c.UpdatedAt, &c.SyncStatus, &row, &c.SheetRange, &c.SyncError, &c.SheetURL, &star, &demo, &job, &c.LegacyRef, &asg, &c.Stage, &c.FollowUpAt)
	if asg.Valid {
		c.AssigneeID = &asg.Int64
	}
	c.Data = json.RawMessage(data)
	if row.Valid {
		c.SheetRow = &row.Int64
	}
	if job.Valid {
		c.JobID = &job.Int64
	}
	c.Starred, c.Demo = star == 1, demo == 1
	return c, err
}

// ErrDuplicate signals a unique identity collision.
var ErrDuplicate = errors.New("duplicate identity")

// InsertCompany stores an accepted company; unique keys prevent duplicates.
func (s *Store) InsertCompany(ctx context.Context, c Company) (Company, error) {
	now := Now()
	demo := 0
	if c.Demo {
		demo = 1
	}
	var job any
	if c.JobID != nil {
		job = *c.JobID
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO companies(name,name_key,domain,linkedin_key,data,partner,created_at,updated_at,sync_status,demo,job_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		c.Name, c.NameKey, nullStr(c.Domain), nullStr(c.LinkedInKey), string(c.Data), c.Partner, now, now, c.SyncStatus, demo, job)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Company{}, ErrDuplicate
		}
		return Company{}, err
	}
	id, _ := res.LastInsertId()
	return s.Company(ctx, id)
}

// Company fetches one company.
func (s *Store) Company(ctx context.Context, id int64) (Company, error) {
	c, err := scanCompany(s.DB.QueryRowContext(ctx, `SELECT `+companyCols+` FROM companies WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Identity keys of every stored company.
func (s *Store) CompanyKeys(ctx context.Context) ([][3]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name_key, COALESCE(domain,''), COALESCE(linkedin_key,'') FROM companies`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][3]string
	for rows.Next() {
		var k [3]string
		if err := rows.Scan(&k[0], &k[1], &k[2]); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ListQuery filters/sorts/paginates lists.
type ListQuery struct {
	Q, Status, Country, Stage, Sort string
	Pipeline                        string // workflow stage filter
	Assignee                        int64  // >0 a user, -1 unassigned
	Desc, Starred                   bool
	Page, PerPage                   int
	IDs                             []int64
}

func (lq *ListQuery) norm() {
	if lq.PerPage <= 0 || lq.PerPage > 500 {
		lq.PerPage = 25
	}
	if lq.Page < 1 {
		lq.Page = 1
	}
}

func (lq ListQuery) workflowFilters(where []string, args []any) ([]string, []any) {
	if lq.Pipeline != "" {
		where = append(where, `stage=?`)
		args = append(args, lq.Pipeline)
	}
	switch {
	case lq.Assignee > 0:
		where = append(where, `assignee_id=?`)
		args = append(args, lq.Assignee)
	case lq.Assignee == -1:
		where = append(where, `assignee_id IS NULL`)
	}
	return where, args
}

func likeEscape(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(s)) + "%"
}

// Companies lists companies.
func (s *Store) Companies(ctx context.Context, lq ListQuery) ([]Company, int, error) {
	lq.norm()
	where := []string{"1=1"}
	var args []any
	if lq.Q != "" {
		where = append(where, `(lower(name) LIKE ? ESCAPE '\' OR lower(partner) LIKE ? ESCAPE '\' OR lower(json_extract(data,'$.sector')) LIKE ? ESCAPE '\' OR lower(COALESCE(domain,'')) LIKE ? ESCAPE '\')`)
		l := likeEscape(lq.Q)
		args = append(args, l, l, l, l)
	}
	if lq.Country != "" {
		where = append(where, `json_extract(data,'$.hq_country')=?`)
		args = append(args, lq.Country)
	}
	if lq.Stage != "" {
		where = append(where, `json_extract(data,'$.stage')=?`)
		args = append(args, lq.Stage)
	}
	if lq.Status != "" {
		where = append(where, `sync_status=?`)
		args = append(args, lq.Status)
	}
	if lq.Starred {
		where = append(where, `starred=1`)
	}
	where, args = lq.workflowFilters(where, args)
	order := map[string]string{"name": "lower(name)", "created": "id", "follow_up": "CASE WHEN follow_up_at='' THEN '9999' ELSE follow_up_at END", "country": "json_extract(data,'$.hq_country')", "funding": "CAST(json_extract(data,'$.total_funding_usd') AS REAL)", "partner": "lower(partner)", "sync": "sync_status"}[lq.Sort]
	if order == "" {
		order, lq.Desc = "id", true
	}
	dir := "ASC"
	if lq.Desc {
		dir = "DESC"
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM companies WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+companyCols+` FROM companies WHERE `+w+` ORDER BY `+order+` `+dir+`, id DESC LIMIT ? OFFSET ?`, append(args, lq.PerPage, (lq.Page-1)*lq.PerPage)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Company{}
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// AllCompanies returns every company (exports, sync).
func (s *Store) AllCompanies(ctx context.Context, where string, args ...any) ([]Company, error) {
	if where == "" {
		where = "1=1"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+companyCols+` FROM companies WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Company{}
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetCompanySync records the outcome of a sync.
func (s *Store) SetCompanySync(ctx context.Context, id int64, status string, row int64, rng, errMsg string) error {
	var r any
	if row > 0 {
		r = row
	}
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE companies SET sync_status=?, sheet_row=COALESCE(?, sheet_row), sheet_range=COALESCE(?, sheet_range), sync_error=?, updated_at=? WHERE id=?`, status, r, nullStr(rng), nullStr(errMsg), Now(), id)
	return err
}

// SetSheetURL stores the link to a verified sheet row.
func (s *Store) SetSheetURL(ctx context.Context, table string, id int64, link string) error {
	if table != "companies" && table != "pitches" {
		return fmt.Errorf("bad table")
	}
	_, err := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE `+table+` SET sheet_url=? WHERE id=?`, link, id)
	return err
}

// Star toggles the shortlist flag on a table row.
func (s *Store) Star(ctx context.Context, table string, id int64, on bool) error {
	if table != "companies" && table != "pitches" {
		return fmt.Errorf("bad table")
	}
	v := 0
	if on {
		v = 1
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE `+table+` SET starred=? WHERE id=?`, v, id)
	return err
}

// ---- candidates ----

// Candidate is every researched outbound company and its outcome.
type Candidate struct {
	ID        int64           `json:"id"`
	JobID     *int64          `json:"job_id"`
	Partner   string          `json:"partner"`
	Name      string          `json:"name"`
	URL       string          `json:"url"`
	Outcome   string          `json:"outcome"` // accepted, rejected, needs_research, duplicate, blocked
	Reasons   []string        `json:"reasons"`
	Record    json.RawMessage `json:"record"`
	Sources   json.RawMessage `json:"sources"`
	CompanyID *int64          `json:"company_id"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

const candCols = `id,job_id,partner,name,url,outcome,reasons,record,sources,company_id,created_at,updated_at`

func scanCand(sc interface{ Scan(...any) error }) (Candidate, error) {
	var c Candidate
	var job, comp sql.NullInt64
	var reasons, rec, src string
	err := sc.Scan(&c.ID, &job, &c.Partner, &c.Name, &c.URL, &c.Outcome, &reasons, &rec, &src, &comp, &c.CreatedAt, &c.UpdatedAt)
	if job.Valid {
		c.JobID = &job.Int64
	}
	if comp.Valid {
		c.CompanyID = &comp.Int64
	}
	_ = json.Unmarshal([]byte(reasons), &c.Reasons)
	if c.Reasons == nil {
		c.Reasons = []string{}
	}
	c.Record, c.Sources = json.RawMessage(rec), json.RawMessage(src)
	return c, err
}

// AddCandidate records a research outcome.
func (s *Store) AddCandidate(ctx context.Context, c Candidate) (int64, error) {
	var job, comp any
	if c.JobID != nil {
		job = *c.JobID
	}
	if c.CompanyID != nil {
		comp = *c.CompanyID
	}
	if c.Record == nil {
		c.Record = json.RawMessage(`{}`)
	}
	if c.Sources == nil {
		c.Sources = json.RawMessage(`[]`)
	}
	now := Now()
	res, err := s.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO candidates(job_id,partner,name,url,outcome,reasons,record,sources,company_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, job, c.Partner, c.Name, c.URL, c.Outcome, js(c.Reasons), string(c.Record), string(c.Sources), comp, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Candidates lists research outcomes.
func (s *Store) Candidates(ctx context.Context, jobID int64, outcome string, limit int) ([]Candidate, error) {
	q := `SELECT ` + candCols + ` FROM candidates WHERE 1=1`
	var args []any
	if jobID > 0 {
		q += ` AND job_id=?`
		args = append(args, jobID)
	}
	if outcome != "" {
		q += ` AND outcome=?`
		args = append(args, outcome)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		c, err := scanCand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Candidate fetches one candidate.
func (s *Store) Candidate(ctx context.Context, id int64) (Candidate, error) {
	c, err := scanCand(s.DB.QueryRowContext(ctx, `SELECT `+candCols+` FROM candidates WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// KnownCandidateURL reports whether a URL was already researched with a final outcome.
func (s *Store) KnownCandidateURL(ctx context.Context, url string) (string, bool) {
	var o string
	err := s.DB.QueryRowContext(ctx, `SELECT outcome FROM candidates WHERE url=? AND outcome IN ('accepted','rejected') ORDER BY id DESC LIMIT 1`, url).Scan(&o)
	return o, err == nil
}
