package httpapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/itsmepava/nventures-sourcing/internal/auth"
	"github.com/itsmepava/nventures-sourcing/internal/jobs"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

// ---- workflow: assignment, stage, follow-up ----

func (a *App) setWorkflow(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		var in store.Workflow
		if !ok || readJSON(r, &in) != nil {
			writeErr(w, 400, "Bad request")
			return
		}
		ctx := r.Context()
		var before *int64
		var name string
		if table == "pitches" {
			p, err := a.Store.Pitch(ctx, id)
			if err != nil {
				writeErr(w, 404, "Not found")
				return
			}
			before, name = p.AssigneeID, p.Name
		} else {
			c, err := a.Store.Company(ctx, id)
			if err != nil {
				writeErr(w, 404, "Not found")
				return
			}
			before, name = c.AssigneeID, c.Name
		}
		if err := a.Store.SetWorkflow(ctx, table, id, in); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		me := store.ActorFrom(ctx)
		entity := map[string]string{"pitches": "pitch", "companies": "company"}[table]
		link := map[string]string{"pitches": "#/inbox?pitch=", "companies": "#/companies?company="}[table] + fmt.Sprint(id)
		msg := fmt.Sprintf("%s updated %s: stage %s", me.Name, name, strings.ReplaceAll(in.Stage, "_", " "))
		if in.FollowUpAt != "" {
			msg += ", follow up " + in.FollowUpAt
		}
		if in.AssigneeID != nil && *in.AssigneeID > 0 && (before == nil || *before != *in.AssigneeID) {
			if u, err := a.Store.User(ctx, *in.AssigneeID); err == nil {
				msg += ", assigned to " + u.Name
				a.Store.Notify(ctx, []int64{u.ID}, nil, "assigned", fmt.Sprintf("%s assigned you %s", me.Name, name), link)
			}
		} else if (in.AssigneeID == nil || *in.AssigneeID == 0) && before != nil {
			msg += ", unassigned"
		}
		a.Store.Audit(ctx, 0, "workflow", msg, map[string]any{entity + "_id": id})
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

// ---- notes ----

func (a *App) listNotes(w http.ResponseWriter, r *http.Request) {
	entity := r.URL.Query().Get("entity")
	id := int64(qInt(r, "id", 0))
	if (entity != "pitch" && entity != "company") || id <= 0 {
		writeErr(w, 400, "entity and id are required")
		return
	}
	ns, err := a.Store.Notes(r.Context(), entity, id)
	if err != nil {
		writeErr(w, 500, "Could not load notes")
		return
	}
	writeJSON(w, 200, ns)
}

func (a *App) addNote(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var in struct {
		Entity string
		ID     int64
		Body   string
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	perm := map[string]string{"pitch": auth.PitchWork, "company": auth.CompanyWork}[in.Entity]
	if perm == "" || in.ID <= 0 {
		writeErr(w, 400, "entity and id are required")
		return
	}
	if !auth.Can(u.Role, perm) {
		writeErr(w, 403, fmt.Sprintf("Your role (%s) can't add notes here.", auth.RoleName(u.Role)))
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || utf8.RuneCountInString(in.Body) > 5000 {
		writeErr(w, 400, "Write a note (up to 5,000 characters)")
		return
	}
	var name string
	if in.Entity == "pitch" {
		p, err := a.Store.Pitch(r.Context(), in.ID)
		if err != nil {
			writeErr(w, 404, "Not found")
			return
		}
		name = p.Name
	} else {
		c, err := a.Store.Company(r.Context(), in.ID)
		if err != nil {
			writeErr(w, 404, "Not found")
			return
		}
		name = c.Name
	}
	n, err := a.Store.AddNote(r.Context(), in.Entity, in.ID, in.Body)
	if err != nil {
		writeErr(w, 500, "Could not save the note")
		return
	}
	a.Store.Audit(r.Context(), 0, "note", fmt.Sprintf("%s added a note on %s", u.Name, name), map[string]any{in.Entity + "_id": in.ID})
	writeJSON(w, 201, n)
}

// ---- queue and notifications ----

func (a *App) queue(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	items, err := a.Store.Queue(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, "Could not load your queue")
		return
	}
	today := time.Now().Format("2006-01-02")
	due := 0
	for _, q := range items {
		if q.FollowUpAt != "" && q.FollowUpAt <= today {
			due++
		}
	}
	writeJSON(w, 200, map[string]any{"items": items, "due": due, "today": today})
}

func (a *App) listNotifications(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	ns, unread, err := a.Store.Notifications(r.Context(), u.ID, 30)
	if err != nil {
		writeErr(w, 500, "Could not load notifications")
		return
	}
	writeJSON(w, 200, map[string]any{"items": ns, "unread": unread})
}

func (a *App) readNotifications(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	var in struct{ IDs []int64 }
	_ = readJSON(r, &in)
	if err := a.Store.MarkNotificationsRead(r.Context(), u.ID, in.IDs); err != nil {
		writeErr(w, 500, "Could not update notifications")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// notifyJobFinished tells the right people when a job ends.
func (a *App) notifyJobFinished(ctx context.Context, j store.Job) {
	label := map[string]string{"inbound_import": "Inbox import", "inbound_sync": "Inbound sync", "outbound_run": "Sourcing run", "outbound_sync": "Outbound sync", "outbound_research": "Company research"}[j.Kind]
	link := fmt.Sprintf("#/activity?job=%d", j.ID)
	msg := fmt.Sprintf("%s #%d %s: %s", label, j.ID, strings.ReplaceAll(j.Status, "_", " "), j.Message)
	_, scheduled := j.Params["schedule_id"]
	switch {
	case scheduled:
		roles := []string{auth.Admin}
		if strings.HasPrefix(j.Kind, "inbound") {
			roles = append(roles, auth.Responder)
		} else {
			roles = append(roles, auth.Sourcer)
		}
		a.Store.Notify(ctx, nil, roles, "job", msg, link)
	case j.CreatedBy != nil && j.Status != "completed":
		// People see their own successful jobs live; only flag problems.
		a.Store.Notify(context.Background(), []int64{*j.CreatedBy}, nil, "job", msg, link)
	}
	if j.Kind == "inbound_import" {
		if f, ok := j.Counters["review"].(float64); ok && f > 0 {
			a.Store.Notify(ctx, nil, []string{auth.Responder}, "review", fmt.Sprintf("%d new pitch(es) need review", int(f)), "#/inbox")
		}
	}
}

// ---- reply templates ----

func (a *App) listTemplates(w http.ResponseWriter, r *http.Request) {
	ts, err := a.Store.Templates(r.Context())
	if err != nil {
		writeErr(w, 500, "Could not load templates")
		return
	}
	writeJSON(w, 200, ts)
}

func (a *App) saveTemplate(w http.ResponseWriter, r *http.Request) {
	var t store.Template
	if err := readJSON(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if r.Method == http.MethodPut {
		id, ok := pathID(r)
		if !ok {
			writeErr(w, 400, "Bad id")
			return
		}
		t.ID = id
	} else {
		t.ID = 0
	}
	t.Name, t.Subject = strings.TrimSpace(t.Name), strings.TrimSpace(t.Subject)
	if !validName(t.Name) || t.Subject == "" || utf8.RuneCountInString(t.Subject) > 200 || strings.TrimSpace(t.Body) == "" || utf8.RuneCountInString(t.Body) > 5000 {
		writeErr(w, 400, "Template needs a name (2–100), a subject (up to 200) and a body (up to 5,000 characters)")
		return
	}
	out, err := a.Store.SaveTemplate(r.Context(), t)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "Template not found")
		return
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	a.Store.Audit(r.Context(), 0, "template", "Reply template saved: "+out.Name, nil)
	writeJSON(w, 200, out)
}

func (a *App) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "Bad id")
		return
	}
	if err := a.Store.DeleteTemplate(r.Context(), id); err != nil {
		writeErr(w, 500, "Could not delete")
		return
	}
	a.Store.Audit(r.Context(), 0, "template", fmt.Sprintf("Reply template #%d deleted", id), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- schedules ----

func (a *App) listSchedules(w http.ResponseWriter, r *http.Request) {
	xs, err := a.Store.Schedules(r.Context())
	if err != nil {
		writeErr(w, 500, "Could not load schedules")
		return
	}
	writeJSON(w, 200, xs)
}

func (a *App) saveSchedule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind         string
		EveryMinutes int `json:"every_minutes"`
		Enabled      bool
		Sync         bool
		Limit        int
		Target       int
		MaxPartners  int `json:"max_partners"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.EveryMinutes < 15 || in.EveryMinutes > 7*24*60 {
		writeErr(w, 400, "Run every 15 minutes to 7 days")
		return
	}
	x := store.Schedule{EveryMinutes: in.EveryMinutes, Enabled: in.Enabled, NextRunAt: time.Now().UTC().Add(time.Duration(in.EveryMinutes) * time.Minute).Format(time.RFC3339Nano)}
	if r.Method == http.MethodPut {
		id, ok := pathID(r)
		old, err := a.Store.Schedule(r.Context(), id)
		if !ok || err != nil {
			writeErr(w, 404, "Schedule not found")
			return
		}
		x.ID, x.Kind = id, old.Kind
	} else {
		x.Kind = in.Kind
	}
	switch x.Kind {
	case "inbound_import":
		x.Params = map[string]any{"limit": float64(max(1, min(in.Limit, 500))), "sync": in.Sync}
		if in.Limit == 0 {
			x.Params["limit"] = float64(50)
		}
	case "outbound_run":
		x.Params = map[string]any{"target": float64(max(1, min(in.Target, 100))), "max_partners": float64(max(1, min(in.MaxPartners, 30))), "sync": in.Sync}
		if in.Target == 0 {
			x.Params["target"] = float64(10)
		}
		if in.MaxPartners == 0 {
			x.Params["max_partners"] = float64(6)
		}
	default:
		writeErr(w, 400, "Choose inbox import or sourcing run")
		return
	}
	cfg := a.JobConfig(r.Context())
	if in.Sync && x.Kind == "inbound_import" && cfg.InboundSheet == "" {
		writeErr(w, 400, "Set the inbound sheet in Settings before scheduling syncs")
		return
	}
	if in.Sync && x.Kind == "outbound_run" && (!a.OutboundSyncEnabled(r.Context()) || cfg.OutboundSheet == "") {
		writeErr(w, 400, "Set and enable the outbound sheet in Settings before scheduling syncs")
		return
	}
	out, err := a.Store.SaveSchedule(r.Context(), x)
	if err != nil {
		writeErr(w, 500, "Could not save the schedule")
		return
	}
	a.Store.Audit(r.Context(), 0, "schedule", fmt.Sprintf("Schedule #%d saved: %s every %d min (%s)", out.ID, out.Kind, out.EveryMinutes, map[bool]string{true: "on", false: "off"}[out.Enabled]), nil)
	writeJSON(w, 200, out)
}

func (a *App) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "Bad id")
		return
	}
	if err := a.Store.DeleteSchedule(r.Context(), id); err != nil {
		writeErr(w, 500, "Could not delete")
		return
	}
	a.Store.Audit(r.Context(), 0, "schedule", fmt.Sprintf("Schedule #%d deleted", id), nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) runScheduleNow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	x, err := a.Store.Schedule(r.Context(), id)
	if !ok || err != nil {
		writeErr(w, 404, "Schedule not found")
		return
	}
	j, err := a.runSchedule(r.Context(), x)
	if errors.Is(err, jobs.ErrBusy) {
		writeErr(w, 409, "A job for this workflow is already running")
		return
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 202, j)
}

func (a *App) runSchedule(ctx context.Context, x store.Schedule) (store.Job, error) {
	params := map[string]any{"schedule_id": x.ID}
	for k, v := range x.Params {
		params[k] = v
	}
	if x.Kind == "inbound_import" {
		if _, err := a.Connect(ctx); err != nil {
			_ = a.Store.MarkScheduleRun(ctx, x.ID, 0, "skipped: Google not connected", time.Now().Add(time.Duration(x.EveryMinutes)*time.Minute))
			return store.Job{}, err
		}
	}
	j, err := a.Jobs.Start(ctx, x.Kind, params)
	next := time.Now().Add(time.Duration(x.EveryMinutes) * time.Minute)
	switch {
	case errors.Is(err, jobs.ErrBusy):
		_ = a.Store.MarkScheduleRun(ctx, x.ID, 0, "deferred: another job was running", time.Now().Add(5*time.Minute))
	case err != nil:
		_ = a.Store.MarkScheduleRun(ctx, x.ID, 0, "failed to start", next)
	default:
		if e := a.Store.MarkScheduleRun(ctx, x.ID, j.ID, "started", next); e != nil {
			a.Log.Error("schedule update failed", "err", e)
		}
	}
	return j, err
}

// scheduler starts due schedules. It runs in the background until stop.
func (a *App) scheduler(stop <-chan struct{}) {
	t := time.NewTicker(a.schedulerTick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			ctx := store.WithActor(context.Background(), store.Actor{Name: "Scheduler"})
			due, err := a.Store.DueSchedules(ctx, time.Now())
			if err != nil {
				continue
			}
			for _, x := range due {
				if _, err := a.runSchedule(ctx, x); err == nil {
					a.Store.Audit(ctx, 0, "schedule", fmt.Sprintf("Scheduled %s started (schedule #%d)", strings.ReplaceAll(x.Kind, "_", " "), x.ID), nil)
				}
			}
		}
	}
}
