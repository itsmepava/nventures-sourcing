package httpapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/itsmepava/nventures-sourcing/internal/jobs"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
)

// AutoIntakeEvery is how often new pitch emails are checked for while the
// app is running.
const AutoIntakeEvery = 5 * time.Minute

// autoIntakeBatch bounds one automatic import; the rest follow on the next
// check, so a large backlog never blocks the app.
const autoIntakeBatch = 50

// AutoIntakeOn reports whether automatic intake is switched on (default on).
func (a *App) AutoIntakeOn(ctx context.Context) bool {
	return a.Store.GetSetting(ctx, "cfg.auto_intake", "true") == "true"
}

// autoIntake runs on every scheduler tick and, at most every
// AutoIntakeEvery, checks Gmail. An import job is started only when there
// are messages the app hasn't seen, so Activity isn't filled with empty
// runs. Pitches that are complete and fit the focus are added to the sheet
// by that job; everything else waits in Needs review.
func (a *App) autoIntake(ctx context.Context, now time.Time) {
	if !a.AutoIntakeOn(ctx) {
		return
	}
	if t, err := time.Parse(time.RFC3339Nano, a.Store.GetSetting(ctx, "state.auto_intake_at", "")); err == nil && now.Sub(t) < AutoIntakeEvery {
		return
	}
	_ = a.Store.SetSetting(ctx, "state.auto_intake_at", now.UTC().Format(time.RFC3339Nano))
	result, problem := a.checkNewPitches(ctx)
	_ = a.Store.SetSetting(ctx, "state.auto_intake_result", result)
	last := a.Store.GetSetting(ctx, "state.auto_intake_problem", "")
	if problem && result != last {
		// Tell admins once per distinct problem, not every 5 minutes.
		a.Store.Notify(ctx, nil, []string{"admin"}, "job", "Automatic pitch intake: "+result, "#/settings")
	}
	if problem {
		_ = a.Store.SetSetting(ctx, "state.auto_intake_problem", result)
	} else if last != "" {
		_ = a.Store.SetSetting(ctx, "state.auto_intake_problem", "")
	}
}

// checkNewPitches returns a short status and whether it is a problem.
func (a *App) checkNewPitches(ctx context.Context) (string, bool) {
	if id, ok := a.Jobs.Running(ctx, "inbound_import"); ok && id > 0 {
		return "an import is already running", false
	}
	cfg := a.JobConfig(ctx)
	conn, err := a.Connect(ctx)
	if err != nil {
		return "waiting for the Google connection (Settings → Google connection)", a.Cfg.Mode == "live" && a.hasClientFile()
	}
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	account, err := conn.Gmail.Profile(cctx)
	if err != nil {
		return "Gmail check failed: " + netx.SafeMessage(err), true
	}
	if cfg.ExpectedAccount != "" && !strings.EqualFold(account, cfg.ExpectedAccount) {
		return "the connected mailbox isn't the expected one; reconnect Google", true
	}
	query := cfg.GmailQuery
	if query == "" {
		query = jobs.DefaultGmailQuery
	}
	refs, _, err := conn.Gmail.List(cctx, query, "", 25)
	if err != nil {
		return "Gmail check failed: " + netx.SafeMessage(err), true
	}
	n := 0
	for _, ref := range refs {
		if _, err := a.Store.PitchByMessage(ctx, jobs.MessageKey(account, ref.ID)); err != nil {
			n++
		}
	}
	if n == 0 {
		return "no new pitches", false
	}
	j, err := a.Jobs.Start(ctx, "inbound_import", map[string]any{"limit": autoIntakeBatch, "sync": cfg.InboundSheet != "", "auto_intake": true})
	switch {
	case errors.Is(err, jobs.ErrBusy):
		return "another job was running; will retry", false
	case err != nil:
		return "could not start the import: " + err.Error(), true
	}
	_ = j
	if n == 1 {
		return "1 new email, importing it now", false
	}
	return "new emails found, importing them now", false
}

func (a *App) hasClientFile() bool {
	_, _, ok := a.clientConfig()
	return ok
}

// autoIntakeStatus is shown in Settings and on the Pitch inbox.
func (a *App) autoIntakeStatus(ctx context.Context) map[string]any {
	return map[string]any{
		"enabled": a.AutoIntakeOn(ctx), "every_minutes": int(AutoIntakeEvery / time.Minute),
		"last_at": a.Store.GetSetting(ctx, "state.auto_intake_at", ""), "last_result": a.Store.GetSetting(ctx, "state.auto_intake_result", ""),
		"problem": a.Store.GetSetting(ctx, "state.auto_intake_problem", "") != "",
	}
}
