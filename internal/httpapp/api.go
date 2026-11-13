package httpapp

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/itsmepava/nventures-sourcing/internal/auth"
	"github.com/itsmepava/nventures-sourcing/internal/domain"
	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/fixtures"
	"github.com/itsmepava/nventures-sourcing/internal/google"
	"github.com/itsmepava/nventures-sourcing/internal/jobs"
	"github.com/itsmepava/nventures-sourcing/internal/llm"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

func (a *App) state(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	running := map[string]any{}
	for _, k := range []string{"inbound_import", "outbound_run"} {
		if id, ok := a.Jobs.Running(ctx, k); ok && id > 0 {
			running[strings.Split(k, "_")[0]] = id
		}
	}
	writeJSON(w, 200, map[string]any{"readiness": a.Readiness(ctx), "running": running, "pitch_counts": a.Store.PitchCounts(ctx), "preferences": a.userPrefs(r), "countries": domain.Countries, "funding_limit": domain.FundingLimitUSD})
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{}
	var companies, verified, starred, partners, relevant, pending int
	db := a.Store.DB
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(sync_status='verified'),0), COALESCE(SUM(starred),0), COALESCE(SUM(sync_status IN ('pending','error','uncertain')),0) FROM companies`).Scan(&companies, &verified, &starred, &pending)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(relevant),0) FROM partners`).Scan(&partners, &relevant)
	byCountry := map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT json_extract(data,'$.hq_country'), COUNT(*) FROM companies GROUP BY 1`)
	if err == nil {
		for rows.Next() {
			var c *string
			var n int
			if rows.Scan(&c, &n) == nil && c != nil {
				byCountry[*c] = n
			}
		}
		rows.Close()
	}
	outcomes := map[string]int{}
	if rows, err := db.QueryContext(ctx, `SELECT outcome, COUNT(*) FROM candidates GROUP BY outcome`); err == nil {
		for rows.Next() {
			var k string
			var n int
			if rows.Scan(&k, &n) == nil {
				outcomes[k] = n
			}
		}
		rows.Close()
	}
	jobsList, _ := a.Store.Jobs(ctx, "", 8)
	events, _ := a.Store.Events(ctx, 0, "", 0, 12)
	out["companies"] = map[string]int{"total": companies, "verified": verified, "starred": starred, "pending_sync": pending}
	out["partners"] = map[string]int{"total": partners, "relevant": relevant}
	out["by_country"] = byCountry
	out["candidates"] = outcomes
	out["pitches"] = a.Store.PitchCounts(ctx)
	out["jobs"] = jobsList
	out["events"] = events
	writeJSON(w, 200, out)
}

// ---- settings ----

var settingKeys = map[string]int{"expected_account": 320, "gmail_query": 1000, "inbound_sheet": 200, "inbound_tab": 120, "outbound_sheet": 200, "outbound_tab": 120, "partner_tab": 120, "control_tab": 120, "outbound_sync": 5, "llm_enabled": 5, "llm_model": 200}

func (a *App) getSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := a.JobConfig(ctx)
	writeJSON(w, 200, map[string]any{
		"mode": a.Cfg.Mode, "editable": a.Cfg.Mode == "live",
		"expected_account": cfg.ExpectedAccount, "gmail_query": firstNonEmpty(cfg.GmailQuery, jobs.DefaultGmailQuery),
		"inbound_sheet": cfg.InboundSheet, "inbound_tab": cfg.InboundTab, "outbound_sheet": cfg.OutboundSheet, "outbound_tab": cfg.OutboundTab,
		"partner_tab": cfg.PartnerTab, "control_tab": cfg.ControlTab, "outbound_sync": a.OutboundSyncEnabled(ctx),
		"llm_enabled": a.Store.GetSetting(ctx, "cfg.llm_enabled", "false") == "true", "llm_model": a.Store.GetSetting(ctx, "cfg.llm_model", ""),
		"max_research": a.Cfg.MaxResearch, "env_file": a.Cfg.EnvFile,
	})
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (a *App) postSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ctx := r.Context()
	for k, v := range in {
		limit, ok := settingKeys[k]
		if !ok {
			writeErr(w, 400, "Unknown setting "+k)
			return
		}
		if a.Cfg.Mode == "demo" && k != "llm_enabled" && k != "llm_model" {
			writeErr(w, 400, "Demo mode uses fixed fixture destinations; switch APP_MODE to live to change them")
			return
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if b, isBool := v.(bool); isBool {
			s = fmt.Sprint(b)
		}
		if utf8.RuneCountInString(s) > limit {
			writeErr(w, 400, k+" is too long")
			return
		}
		if k == "expected_account" && s != "" && !strings.Contains(s, "@") {
			writeErr(w, 400, "Expected account must be an email address")
			return
		}
		if k == "llm_model" && s != "" {
			ok := false
			for _, m := range llm.Detect(ctx, a.Cfg.OllamaURL).Models {
				if m.Name == s && m.Local {
					ok = true
				}
			}
			if !ok {
				writeErr(w, 400, "Choose an installed local model")
				return
			}
		}
		if err := a.Store.SetSetting(ctx, "cfg."+k, s); err != nil {
			writeErr(w, 500, "Could not save settings")
			return
		}
	}
	a.Store.Audit(ctx, 0, "settings", "Settings updated: "+strings.Join(keys(in), ", "), nil)
	a.getSettings(w, r)
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (a *App) postPreferences(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	for k, v := range in {
		if len(k) > 40 || len(v) > 4000 {
			writeErr(w, 400, "Preference too large")
			return
		}
		_ = a.Store.SetSetting(r.Context(), a.prefPrefix(r)+k, v)
	}
	writeJSON(w, 200, a.userPrefs(r))
}

// Preferences are stored per person.
func (a *App) prefPrefix(r *http.Request) string {
	u, _ := currentUser(r)
	return fmt.Sprintf("pref.u%d.", u.ID)
}

func (a *App) userPrefs(r *http.Request) map[string]string {
	out := map[string]string{}
	p := a.prefPrefix(r)
	for k, v := range a.Store.Settings(r.Context(), p) {
		out["pref."+strings.TrimPrefix(k, p)] = v
	}
	return out
}

// ---- pitches ----

func listQuery(r *http.Request) store.ListQuery {
	q := r.URL.Query()
	lq := store.ListQuery{Q: strings.TrimSpace(q.Get("q")), Status: q.Get("status"), Country: q.Get("country"), Stage: q.Get("stage"), Pipeline: q.Get("pipeline"), Sort: q.Get("sort"), Desc: q.Get("desc") == "1", Starred: q.Get("starred") == "1", Page: qInt(r, "page", 1), PerPage: qInt(r, "per_page", 25)}
	switch a := q.Get("assignee"); a {
	case "":
	case "me":
		if u, ok := currentUser(r); ok {
			lq.Assignee = u.ID
		}
	case "none":
		lq.Assignee = -1
	default:
		lq.Assignee = int64(qInt(r, "assignee", 0))
	}
	return lq
}

func (a *App) listPitches(w http.ResponseWriter, r *http.Request) {
	items, total, err := a.Store.Pitches(r.Context(), listQuery(r))
	if err != nil {
		writeErr(w, 500, "Could not list pitches")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

func (a *App) getPitch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	p, err := a.Store.Pitch(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "Pitch not found")
		return
	}
	ops, _ := a.Store.SyncOps(r.Context(), "pitch", id, 20)
	writeJSON(w, 200, map[string]any{"pitch": p, "sync_ops": ops})
}

var lockedPitch = map[string]bool{"verified": true, "already_exists": true, "demo_verified": true, "syncing": true}

func (a *App) reviewPitch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	ctx := r.Context()
	p, err := a.Store.Pitch(ctx, id)
	if !ok || err != nil {
		writeErr(w, 404, "Pitch not found")
		return
	}
	if lockedPitch[p.Status] {
		writeErr(w, 409, "This pitch is already in the sheet (or being written). Edit the sheet directly; nVentures never overwrites sheet rows.")
		return
	}
	var in struct {
		Name, Description, HQ, Contact, Website, Notes, Decision string
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	switch in.Decision {
	case "ready", "skipped", "review":
	default:
		writeErr(w, 400, "Decision must be ready, review or skipped")
		return
	}
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if in.Decision == "ready" && (in.Name == "" || utf8.RuneCountInString(in.Description) < 10 || !strings.Contains(in.Contact, "@")) {
		writeErr(w, 400, "To approve, provide a company name, a description of at least 10 characters and a contact email")
		return
	}
	for _, f := range []struct {
		v string
		n int
	}{{in.Name, 300}, {in.Description, 12000}, {in.HQ, 300}, {in.Contact, 500}, {in.Website, 500}, {in.Notes, 12000}} {
		if utf8.RuneCountInString(f.v) > f.n {
			writeErr(w, 400, "A field is too long")
			return
		}
	}
	var d extract.Pitch
	_ = json.Unmarshal(p.Data, &d)
	d.Name, d.Description, d.HQ, d.Contact, d.Website, d.Notes = in.Name, in.Description, strings.TrimSpace(in.HQ), strings.TrimSpace(in.Contact), strings.TrimSpace(in.Website), in.Notes
	d.Status = in.Decision
	p.Data, _ = json.Marshal(d)
	p.Name, p.NameKey, p.Domain, p.Status, p.Error = d.Name, domain.NameKey(d.Name), domain.IdentityDomain(d.Website), in.Decision, ""
	if err := a.Store.UpdatePitch(ctx, p); err != nil {
		writeErr(w, 500, "Could not save")
		return
	}
	a.Store.Audit(ctx, 0, "inbox_review", fmt.Sprintf("Pitch #%d %s: %s", p.ID, p.Name, in.Decision), map[string]any{"pitch_id": p.ID})
	writeJSON(w, 200, p)
}

func (a *App) star(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		var in struct{ On bool }
		if !ok || readJSON(r, &in) != nil {
			writeErr(w, 400, "Bad request")
			return
		}
		if err := a.Store.Star(r.Context(), table, id, in.On); err != nil {
			writeErr(w, 500, "Could not update shortlist")
			return
		}
		writeJSON(w, 200, map[string]any{"id": id, "starred": in.On})
	}
}

func (a *App) suggestPitch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if a.Store.GetSetting(ctx, "cfg.llm_enabled", "false") != "true" {
		writeErr(w, 400, "Local model suggestions are off. Enable them in Settings (optional).")
		return
	}
	id, ok := pathID(r)
	p, err := a.Store.Pitch(ctx, id)
	if !ok || err != nil {
		writeErr(w, 404, "Pitch not found")
		return
	}
	var d extract.Pitch
	_ = json.Unmarshal(p.Data, &d)
	model := a.Store.GetSetting(ctx, "cfg.llm_model", "")
	s, err := llm.Suggest(ctx, a.Cfg.OllamaURL, model, d.Excerpt+"\n"+d.Description)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	a.Store.Audit(ctx, 0, "llm_suggestion", fmt.Sprintf("Local model %s suggested fields for pitch #%d (not applied automatically)", model, id), nil)
	writeJSON(w, 200, s)
}

func (a *App) llmStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, llm.Detect(r.Context(), a.Cfg.OllamaURL))
}

// ---- companies & candidates ----

func (a *App) listCompanies(w http.ResponseWriter, r *http.Request) {
	items, total, err := a.Store.Companies(r.Context(), listQuery(r))
	if err != nil {
		writeErr(w, 500, "Could not list companies")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

func (a *App) getCompany(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	c, err := a.Store.Company(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "Company not found")
		return
	}
	ops, _ := a.Store.SyncOps(r.Context(), "company", id, 20)
	reasons := domain.Qualifies(recordOf(c.Data))
	if reasons == nil {
		reasons = []string{}
	}
	writeJSON(w, 200, map[string]any{"company": c, "sync_ops": ops, "qualification": reasons})
}

func recordOf(raw json.RawMessage) domain.Record {
	var rec domain.Record
	_ = json.Unmarshal(raw, &rec)
	return rec
}

func (a *App) listCandidates(w http.ResponseWriter, r *http.Request) {
	jobID := int64(qInt(r, "job_id", 0))
	items, err := a.Store.Candidates(r.Context(), jobID, r.URL.Query().Get("outcome"), min(max(qInt(r, "limit", 200), 1), 1000))
	if err != nil {
		writeErr(w, 500, "Could not list candidates")
		return
	}
	writeJSON(w, 200, items)
}

func (a *App) getCandidate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	c, err := a.Store.Candidate(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "Not found")
		return
	}
	writeJSON(w, 200, c)
}

// ---- partners ----

func (a *App) listPartners(w http.ResponseWriter, r *http.Request) {
	ps, err := a.Store.Partners(r.Context(), false, 0)
	if err != nil {
		writeErr(w, 500, "Could not list partners")
		return
	}
	writeJSON(w, 200, ps)
}

func validWebsite(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || len(s) > 500 {
		return "", errors.New("Website must be an http(s) URL")
	}
	return u.String(), nil
}

func (a *App) addPartner(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Website string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if utf8.RuneCountInString(in.Name) < 2 || utf8.RuneCountInString(in.Name) > 200 {
		writeErr(w, 400, "Partner name must be 2–200 characters")
		return
	}
	site, err := validWebsite(in.Website)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	p, err := a.Store.AddPartner(r.Context(), in.Name, site, true)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	a.Store.Audit(r.Context(), 0, "partner_added", "Partner added: "+p.Name, nil)
	writeJSON(w, 201, p)
}

func (a *App) updatePartner(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		Website  *string
		Relevant *bool
	}
	if !ok || readJSON(r, &in) != nil {
		writeErr(w, 400, "Bad request")
		return
	}
	if in.Website != nil {
		site, err := validWebsite(*in.Website)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		in.Website = &site
	}
	p, err := a.Store.UpdatePartner(r.Context(), id, in.Website, in.Relevant)
	if err != nil {
		writeErr(w, 404, "Partner not found")
		return
	}
	a.Store.Audit(r.Context(), 0, "partner_updated", fmt.Sprintf("%s updated (included: %v)", p.Name, p.Relevant), nil)
	writeJSON(w, 200, p)
}

// ---- jobs ----

func (a *App) startJob(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		params := map[string]any{}
		if err := readJSON(r, &params); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		cfg := a.JobConfig(ctx)
		switch kind {
		case "inbound_import":
			if _, err := a.Connect(ctx); err != nil {
				writeErr(w, 400, netx.SafeMessage(err))
				return
			}
			if a.Cfg.Mode == "live" && cfg.ExpectedAccount == "" {
				writeErr(w, 400, "Set the expected Gmail account in Settings first, so the wrong mailbox is never read")
				return
			}
			if s, _ := params["sync"].(bool); s && cfg.InboundSheet == "" {
				writeErr(w, 400, "Set the inbound sheet ID in Settings to sync, or import without syncing")
				return
			}
		case "inbound_sync":
			if cfg.InboundSheet == "" {
				writeErr(w, 400, "Set the inbound sheet ID and tab in Settings first. Nothing was written.")
				return
			}
			if _, err := a.Connect(ctx); err != nil {
				writeErr(w, 400, netx.SafeMessage(err))
				return
			}
		case "outbound_run", "outbound_sync":
			wantSync, _ := params["sync"].(bool)
			if kind == "outbound_sync" {
				wantSync = true
			}
			if wantSync && !a.OutboundSyncEnabled(ctx) {
				writeErr(w, 400, "Outbound sheet writing is disabled. Enable it in Settings (SHEETS_SYNC_ENABLED) after checking the destination.")
				return
			}
			if wantSync && cfg.OutboundSheet == "" {
				writeErr(w, 400, "Set the outbound sheet ID in Settings first. Nothing was written.")
				return
			}
		case "outbound_research":
			urls, _ := params["urls"].([]any)
			if len(urls) == 0 || len(urls) > 50 {
				writeErr(w, 400, "Provide 1–50 company URLs")
				return
			}
		}
		job, err := a.Jobs.Start(ctx, kind, params)
		if errors.Is(err, jobs.ErrBusy) {
			writeErr(w, 409, "A job for this workflow is already running. Wait for it or cancel it in Activity.")
			return
		}
		if err != nil {
			writeErr(w, 500, "Could not start job")
			return
		}
		writeJSON(w, 202, job)
	}
}

func (a *App) listJobs(w http.ResponseWriter, r *http.Request) {
	js, err := a.Store.Jobs(r.Context(), r.URL.Query().Get("kind"), min(max(qInt(r, "limit", 50), 1), 500))
	if err != nil {
		writeErr(w, 500, "Could not list jobs")
		return
	}
	writeJSON(w, 200, js)
}

func (a *App) getJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	j, err := a.Store.Job(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "Job not found")
		return
	}
	ev, _ := a.Store.Events(r.Context(), id, "", int64(qInt(r, "after", 0)), 500)
	writeJSON(w, 200, map[string]any{"job": j, "events": ev})
}

// mayControl checks the person's role may run this job's workflow.
func mayControl(w http.ResponseWriter, r *http.Request, kind string) bool {
	u, _ := currentUser(r)
	if !auth.Can(u.Role, auth.JobPermission(kind)) {
		writeErr(w, 403, fmt.Sprintf("Your role (%s) can't control this kind of job.", auth.RoleName(u.Role)))
		return false
	}
	return true
}

func (a *App) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	j0, err := a.Store.Job(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "Job not found")
		return
	}
	if !mayControl(w, r, j0.Kind) {
		return
	}
	if err := a.Jobs.Cancel(r.Context(), id); err != nil {
		writeErr(w, 500, "Could not cancel")
		return
	}
	j, _ := a.Store.Job(r.Context(), id)
	writeJSON(w, 200, j)
}

// retryJob starts a new job of the same kind with the same parameters.
func (a *App) retryJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	j, err := a.Store.Job(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "Job not found")
		return
	}
	switch j.Status {
	case "failed", "cancelled", "interrupted", "partial", "completed_with_errors":
	default:
		writeErr(w, 400, "Only failed, cancelled, interrupted, partial or error-containing jobs can be retried")
		return
	}
	if !mayControl(w, r, j.Kind) {
		return
	}
	if _, legacy := j.Params["legacy"]; legacy {
		writeErr(w, 400, "Jobs imported from the Python app cannot be retried; start a new one")
		return
	}
	params := map[string]any{}
	for k, v := range j.Params {
		params[k] = v
	}
	params["retry_of"] = j.ID
	delete(params, "schedule_id")
	body, _ := json.Marshal(params)
	r2 := r.Clone(r.Context())
	r2.Body = io.NopCloser(strings.NewReader(string(body)))
	a.startJob(j.Kind)(w, r2)
}

// related returns records in other modules for the same company identity.
func (a *App) related(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nameKey, dom := domain.NameKey(q.Get("name")), domain.IdentityDomain(q.Get("website"))
	rel, err := a.Store.RelatedTo(r.Context(), nameKey, dom, int64(qInt(r, "company", 0)), int64(qInt(r, "pitch", 0)))
	if err != nil {
		writeErr(w, 500, "Could not load related records")
		return
	}
	writeJSON(w, 200, rel)
}

// demoSheet returns a fixture sheet tab so the frontend can show demo row links.
func (a *App) demoSheet(w http.ResponseWriter, r *http.Request) {
	if a.Fixture == nil {
		writeErr(w, 404, "Only available in demo mode")
		return
	}
	id, tab := r.URL.Query().Get("id"), r.URL.Query().Get("tab")
	rows := a.Fixture.Rows(id, tab)
	if rows == nil {
		writeErr(w, 404, "Sheet tab not found")
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "tab": tab, "rows": rows})
}

func (a *App) listEvents(w http.ResponseWriter, r *http.Request) {
	ev, err := a.Store.Events(r.Context(), 0, r.URL.Query().Get("kind"), int64(qInt(r, "after", 0)), min(max(qInt(r, "limit", 200), 1), 1000))
	if err != nil {
		writeErr(w, 500, "Could not list activity")
		return
	}
	writeJSON(w, 200, ev)
}

func (a *App) listSyncOps(w http.ResponseWriter, r *http.Request) {
	ops, err := a.Store.SyncOps(r.Context(), r.URL.Query().Get("target"), int64(qInt(r, "entity_id", 0)), 200)
	if err != nil {
		writeErr(w, 500, "Could not list sync operations")
		return
	}
	writeJSON(w, 200, ops)
}

// ---- CSV ----

// SafeCell neutralises spreadsheet formula injection in exported CSV.
func SafeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func writeCSV(w http.ResponseWriter, name string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	for _, r := range rows {
		for i := range r {
			r[i] = SafeCell(r[i])
		}
		_ = cw.Write(r)
	}
	cw.Flush()
}

func (a *App) exportCompanies(w http.ResponseWriter, r *http.Request) {
	lq := listQuery(r)
	lq.PerPage, lq.Page = 500, 1
	var rows [][]string
	for {
		items, total, err := a.Store.Companies(r.Context(), lq)
		if err != nil {
			writeErr(w, 500, "Export failed")
			return
		}
		for _, c := range items {
			rec := recordOf(c.Data)
			funding := ""
			if rec.TotalFundingUSD != nil {
				funding = fmt.Sprintf("%.0f", *rec.TotalFundingUSD)
			}
			row := ""
			if c.SheetRow != nil {
				row = fmt.Sprint(*c.SheetRow)
			}
			rows = append(rows, []string{c.Name, rec.HQCountry, rec.Sector, rec.Stage, funding, rec.Website, c.Partner, row, c.SyncStatus, fmt.Sprint(c.Starred), c.CreatedAt})
		}
		if lq.Page*lq.PerPage >= total {
			break
		}
		lq.Page++
	}
	writeCSV(w, "nventures-companies.csv", []string{"Company Name", "Country", "Sector", "Stage", "Total Funding USD", "Website", "Partner", "Sheet Row", "Sync Status", "Shortlisted", "Saved At"}, rows)
}

func (a *App) exportPitches(w http.ResponseWriter, r *http.Request) {
	lq := listQuery(r)
	lq.PerPage, lq.Page = 500, 1
	var rows [][]string
	for {
		items, total, err := a.Store.Pitches(r.Context(), lq)
		if err != nil {
			writeErr(w, 500, "Export failed")
			return
		}
		for _, p := range items {
			var d extract.Pitch
			_ = json.Unmarshal(p.Data, &d)
			rows = append(rows, []string{d.Name, d.Description, d.HQ, d.Contact, d.Website, p.Status, d.Received, fmt.Sprint(p.Starred)})
		}
		if lq.Page*lq.PerPage >= total {
			break
		}
		lq.Page++
	}
	writeCSV(w, "nventures-pitches.csv", []string{"Company Name", "Description", "HQ (sender-reported)", "Contact", "Website", "Status", "Received", "Shortlisted"}, rows)
}

// ---- Google connection ----

func (a *App) uploadClient(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.Mode == "demo" {
		writeErr(w, 400, "Demo mode uses a fixture Google connection; nothing to upload")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeErr(w, 400, "Could not read upload")
		return
	}
	var wrap struct{ JSON string }
	if json.Unmarshal(body, &wrap) == nil && wrap.JSON != "" {
		body = []byte(wrap.JSON)
	}
	if _, err := google.ParseClientJSON(body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	tmp := a.clientPath() + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil || os.Rename(tmp, a.clientPath()) != nil {
		writeErr(w, 500, "Could not store the client file")
		return
	}
	a.Store.Audit(r.Context(), 0, "google_client", "OAuth client file stored locally (user-only permissions)", nil)
	writeJSON(w, 200, a.Readiness(r.Context()))
}

func (a *App) disconnect(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.Mode == "demo" {
		writeErr(w, 400, "Demo connection is a fixture")
		return
	}
	err := os.Remove(a.tokenPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeErr(w, 500, "Could not remove the local token")
		return
	}
	a.Store.Audit(r.Context(), 0, "google_disconnect", "Local Google token removed. Revoke access at myaccount.google.com/permissions if you also want Google to forget it.", nil)
	writeJSON(w, 200, a.Readiness(r.Context()))
}

func (a *App) redirectURI(r *http.Request) string {
	if a.Cfg.BaseURL != "" {
		return a.Cfg.BaseURL + "/oauth/callback"
	}
	return fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", a.Port)
}

func (a *App) oauthStart(w http.ResponseWriter, r *http.Request) {
	client, _, ok := a.clientConfig()
	if !ok {
		http.Redirect(w, r, "/#/settings?google=missing-client", http.StatusSeeOther)
		return
	}
	p := google.NewPKCE()
	a.oauthMu.Lock()
	for k, v := range a.pending {
		if time.Since(v.Created) > 10*time.Minute {
			delete(a.pending, k)
		}
	}
	a.pending[p.State] = p
	a.oauthMu.Unlock()
	http.Redirect(w, r, google.AuthURL(a.Endpoints().Auth, client, a.redirectURI(r), a.JobConfig(r.Context()).ExpectedAccount, p), http.StatusFound)
}

func (a *App) oauthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a.oauthMu.Lock()
	p, ok := a.pending[q.Get("state")]
	delete(a.pending, q.Get("state"))
	a.oauthMu.Unlock()
	fail := func(msg string) {
		a.Store.Audit(r.Context(), 0, "google_connect_failed", msg, nil)
		http.Redirect(w, r, "/#/settings?google=error&message="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if !ok || time.Since(p.Created) > 10*time.Minute {
		fail("Connection attempt expired or did not match; start again from Settings")
		return
	}
	if e := q.Get("error"); e != "" {
		fail("Google consent was not granted (" + e + ")")
		return
	}
	client, _, _ := a.clientConfig()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	refresh, _, err := google.Exchange(ctx, a.hc, a.Endpoints().Token, client, a.redirectURI(r), q.Get("code"), p)
	if err != nil {
		fail(netx.SafeMessage(err))
		return
	}
	ts := google.RefreshTokenSource(a.hc, a.Endpoints().Token, client.ClientID, client.ClientSecret, refresh)
	gm := &google.Gmail{Base: a.Endpoints().Gmail, HTTP: a.hc, Tokens: ts}
	account, err := gm.Profile(ctx)
	if err != nil {
		fail("Connected, but the Gmail profile could not be read: " + netx.SafeMessage(err))
		return
	}
	expected := a.JobConfig(r.Context()).ExpectedAccount
	if expected != "" && !strings.EqualFold(expected, account) {
		fail("You signed in as a different Google account than the expected mailbox. Nothing was saved.")
		return
	}
	if a.Cfg.Mode == "live" {
		b, _ := json.Marshal(tokenFile{RefreshToken: refresh, Account: strings.ToLower(account), CreatedAt: time.Now().UTC().Format(time.RFC3339)})
		tmp := a.tokenPath() + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil || os.Rename(tmp, a.tokenPath()) != nil {
			fail("Could not store the token locally")
			return
		}
		if expected == "" {
			_ = a.Store.SetSetting(r.Context(), "cfg.expected_account", strings.ToLower(account))
		}
	}
	a.Store.Audit(r.Context(), 0, "google_connected", "Google connected for "+account+" (Gmail read-only, Sheets)", nil)
	http.Redirect(w, r, "/#/settings?google=connected", http.StatusSeeOther)
}

func (a *App) testConnection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	type check struct {
		Name, Status, Message string
	}
	var out []check
	conn, err := a.Connect(ctx)
	if err != nil {
		writeJSON(w, 200, []check{{"Google authorization", "error", netx.SafeMessage(err)}})
		return
	}
	cfg := a.JobConfig(ctx)
	if acct, err := conn.Gmail.Profile(ctx); err != nil {
		out = append(out, check{"Gmail (read-only)", "error", netx.SafeMessage(err)})
	} else if cfg.ExpectedAccount != "" && !strings.EqualFold(acct, cfg.ExpectedAccount) {
		out = append(out, check{"Gmail (read-only)", "error", "Connected mailbox does not match the expected account"})
	} else {
		out = append(out, check{"Gmail (read-only)", "ok", "Mailbox " + acct + " readable"})
	}
	if cfg.InboundSheet == "" {
		out = append(out, check{"Inbound sheet", "missing", "Set the inbound sheet ID"})
	} else if rows, err := conn.Sheets.Values(ctx, cfg.InboundSheet, google.QuoteTab(cfg.InboundTab)+"!A1:F1"); err != nil {
		out = append(out, check{"Inbound sheet", "error", netx.SafeMessage(err)})
	} else if err := google.ValidateInbound(rows); err != nil {
		out = append(out, check{"Inbound sheet", "error", netx.SafeMessage(err)})
	} else {
		out = append(out, check{"Inbound sheet", "ok", "Six expected headers found. No rows were written."})
	}
	if cfg.OutboundSheet == "" {
		out = append(out, check{"Outbound sheet", "missing", "Set the outbound sheet ID (optional until you sync sourced companies)"})
	} else if rows, err := conn.Sheets.Values(ctx, cfg.OutboundSheet, google.QuoteTab(cfg.OutboundTab)+"!A1:AZ1"); err != nil {
		out = append(out, check{"Outbound sheet", "error", netx.SafeMessage(err)})
	} else if err := google.ValidateOutbound(rows); err != nil {
		out = append(out, check{"Outbound sheet", "error", netx.SafeMessage(err)})
	} else {
		out = append(out, check{"Outbound sheet", "ok", fmt.Sprintf("%d headers mapped. No rows were written.", len(rows[0]))})
	}
	a.Store.Audit(ctx, 0, "connection_check", "Read-only connection checks run", out)
	writeJSON(w, 200, out)
}

func (a *App) clearCache(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.ClearCache(r.Context())
	if err != nil {
		writeErr(w, 500, "Could not clear cache")
		return
	}
	writeJSON(w, 200, map[string]any{"cleared": n})
}

// ---- demo helpers ----

func (a *App) demoReset(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.Mode != "demo" || a.Fixture == nil {
		writeErr(w, 400, "Reset is only available for demo data")
		return
	}
	for _, k := range []string{"inbound_import", "outbound_run"} {
		if id, ok := a.Jobs.Running(r.Context(), k); ok && id > 0 {
			writeErr(w, 409, "Wait for running jobs to finish first")
			return
		}
	}
	ctx := r.Context()
	backup := fmt.Sprintf("%s/backups/demo-before-reset-%s.db", a.Cfg.ModeDir(), time.Now().UTC().Format("20060102-150405"))
	if err := a.Store.Backup(ctx, backup); err != nil {
		writeErr(w, 500, "Backup before reset failed; nothing was reset")
		return
	}
	for _, t := range []string{"candidates", "sync_ops", "companies", "pitches", "events", "jobs", "partners", "fetch_cache", "mailbox_state"} {
		if _, err := a.Store.DB.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			writeErr(w, 500, "Reset failed")
			return
		}
	}
	fresh := fixtures.NewServer("")
	for id, book := range fresh.Sheets {
		for tab, rows := range book {
			a.Fixture.SetRows(id, tab, rows)
		}
	}
	a.seedDemo(ctx)
	a.Store.Audit(ctx, 0, "demo_reset", "Demo data reset (backup kept at "+backup+")", nil)
	writeJSON(w, 200, map[string]any{"ok": true, "backup": backup})
}

func (a *App) demoSite(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("host")
	if a.Fixture == nil || !strings.HasSuffix(host, fixtures.DemoSuffix) {
		http.NotFound(w, r)
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, a.Fixture.URL+"/"+r.PathValue("path"), nil)
	req.Host = host
	req.URL.RawQuery = r.URL.RawQuery
	resp, err := a.hc.Do(req)
	if err != nil {
		http.Error(w, "fixture unavailable", 502)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 2<<20))
}
