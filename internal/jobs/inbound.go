package jobs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/itsmepava/nventures-sourcing/internal/domain"
	"github.com/itsmepava/nventures-sourcing/internal/extract"
	"github.com/itsmepava/nventures-sourcing/internal/google"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

// MaxListPages bounds how far back one import scans the mailbox.
const MaxListPages = 20

// DefaultGmailQuery finds likely pitches while excluding spam/trash/sent.
const DefaultGmailQuery = `-in:spam -in:trash -in:sent {"Company name" "pitch deck" subject:pitch subject:fundraising subject:introducing "seed round" "pre-seed"}`

// MessageKey is the per-account Gmail message identity.
func MessageKey(account, id string) string { return strings.ToLower(account) + ":" + id }

func (r *run) connectGoogle() (*Conn, error) {
	conn, err := r.env.Connect(r.ctx)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (r *run) verifyMailbox(conn *Conn) (string, error) {
	account, err := conn.Gmail.Profile(r.ctx)
	if err != nil {
		return "", err
	}
	if r.cfg.ExpectedAccount != "" && !strings.EqualFold(account, r.cfg.ExpectedAccount) {
		return "", &netx.ProviderError{Msg: "Connected Gmail account does not match the expected mailbox. Reconnect with the expected account."}
	}
	return strings.ToLower(account), nil
}

func (r *run) inboundImport(p map[string]any) {
	limit := intParam(p, "limit", 50, 1, 500)
	syncAfter := boolParam(p, "sync")
	conn, err := r.connectGoogle()
	if err != nil {
		r.fail(err)
		return
	}
	account, err := r.verifyMailbox(conn)
	if err != nil {
		r.fail(err)
		return
	}
	query := r.cfg.GmailQuery
	if query == "" {
		query = DefaultGmailQuery
	}
	r.save(2, "Listing matching Gmail messages")
	// Identity is account + message ID, never the thread: Gmail groups
	// unrelated website submissions into one thread.
	var refs []google.MessageRef
	page, pages, more := "", 0, false
	for len(refs) < limit {
		if r.cancelled() {
			break
		}
		batch, next, err := conn.Gmail.List(r.ctx, query, page, 100)
		if err != nil {
			r.fail(err)
			return
		}
		pages++
		r.inc("listed", len(batch))
		for _, m := range batch {
			if _, err := r.env.Store.PitchByMessage(r.ctx, MessageKey(account, m.ID)); err == nil {
				r.inc("already_imported", 1)
				continue
			}
			if len(refs) >= limit {
				more = true
				break
			}
			refs = append(refs, m)
		}
		if next == "" {
			break
		}
		page = next
		if len(refs) >= limit {
			more = true
			break
		}
		if pages >= MaxListPages {
			more = true
			break
		}
	}
	r.counters["more_available"] = more
	r.counters["pages_listed"] = pages
	_, _ = r.env.Store.DB.ExecContext(r.ctx, `INSERT INTO mailbox_state(account,last_scan_at,more_available,listed) VALUES(?,?,?,?) ON CONFLICT(account) DO UPDATE SET last_scan_at=excluded.last_scan_at, more_available=excluded.more_available, listed=excluded.listed`, account, store.Now(), map[bool]int{true: 1, false: 0}[more], r.get("listed"))
	// Gmail lists newest first; import oldest first so the original submission
	// is kept and later re-submissions are the ones marked duplicate.
	for i, j := 0, len(refs)-1; i < j; i, j = i+1, j-1 {
		refs[i], refs[j] = refs[j], refs[i]
	}
	for i, ref := range refs {
		if r.cancelled() {
			break
		}
		r.save(5+int(float64(i)/float64(max(len(refs), 1))*(map[bool]float64{true: 60, false: 90}[syncAfter])), fmt.Sprintf("Reading message %d of %d", i+1, len(refs)))
		msg, err := conn.Gmail.Get(r.ctx, ref.ID)
		if err != nil {
			r.inc("errors", 1)
			r.audit("inbox_error", "Message "+ref.ID+": "+netx.SafeMessage(err)+" (not saved; the next import retries it)", nil)
			continue
		}
		r.importMessage(account, msg)
	}
	if syncAfter && !r.cancelled() {
		r.save(70, "Syncing ready pitches to the inbound sheet")
		r.syncPitches(nil, conn)
	}
	status := "completed"
	switch {
	case r.cancelled():
		status = "cancelled"
	case r.get("errors") > 0 || r.get("sync_failed") > 0:
		status = "completed_with_errors"
	}
	msg := fmt.Sprintf("%d new · %d ready · %d review · %d skipped · %d duplicates · %d errors", r.get("imported"), r.get("ready"), r.get("review"), r.get("skipped"), r.get("duplicates"), r.get("errors"))
	if syncAfter {
		msg += fmt.Sprintf(" · %d verified in sheet · %d already in sheet · %d sync failures", r.get("verified"), r.get("already_exists"), r.get("sync_failed"))
	}
	if more {
		msg += " · more matching messages remain; run again to continue"
	}
	r.finish(status, msg)
}

func (r *run) importMessage(account string, msg extract.GmailMessage) {
	p := extract.Extract(extract.FromGmail(msg))
	p.MessageID, p.ThreadID = msg.ID, msg.ThreadID
	p.Demo = r.cfg.Mode == "demo"
	if r.cfg.Mode == "demo" {
		p.GmailURL = ""
	} else {
		p.GmailURL = "https://mail.google.com/mail/u/?authuser=" + account + "#all/" + msg.ID
	}
	key := MessageKey(account, msg.ID)
	nameKey := domain.NameKey(p.Name)
	dom := domain.IdentityDomain(p.Website)
	status := p.Status
	var dupOf *int64
	if p.FollowUp {
		if id, ok := r.env.Store.ThreadHasPitch(r.ctx, msg.ThreadID, 0); ok {
			status = "skipped"
			p.Reasons = append(p.Reasons, fmt.Sprintf("Follow-up in the conversation of pitch #%d; no new company introduced", id))
		}
	}
	if status != "skipped" && !p.NameGuessed {
		if id, ok := r.env.Store.FindPitchIdentity(r.ctx, nameKey, dom, 0); ok {
			status = "duplicate"
			dupOf = &id
			p.Reasons = append(p.Reasons, fmt.Sprintf("Same company as pitch #%d (name or website)", id))
		}
	}
	p.Status = status
	data, _ := json.Marshal(p)
	jobID := r.job.ID
	_, err := r.env.Store.InsertPitch(r.ctx, store.Pitch{MessageID: key, Account: account, ThreadID: msg.ThreadID, Mode: r.cfg.Mode, Name: p.Name, NameKey: nameKey, Domain: dom, Status: status, Data: data, ReceivedAt: p.Received, DuplicateOf: dupOf, JobID: &jobID})
	if err == store.ErrDuplicate {
		r.inc("already_imported", 1)
		return
	}
	if err != nil {
		r.inc("errors", 1)
		r.audit("inbox_error", "Could not save message "+msg.ID, nil)
		return
	}
	r.inc("imported", 1)
	switch status {
	case "ready", "review", "skipped":
		r.inc(status, 1)
	case "duplicate":
		r.inc("duplicates", 1)
	}
}

func (r *run) inboundSyncJob(p map[string]any) {
	conn, err := r.connectGoogle()
	if err != nil {
		r.fail(err)
		return
	}
	r.syncPitches(idsParam(p), conn)
	status := "completed"
	if r.cancelled() {
		status = "cancelled"
	} else if r.get("sync_failed") > 0 {
		status = "completed_with_errors"
	}
	if f, ok := r.counters["fatal"].(string); ok {
		r.finish("failed", f)
		return
	}
	r.finish(status, fmt.Sprintf("%d verified in sheet · %d already in sheet · %d need attention", r.get("verified"), r.get("already_exists"), r.get("sync_failed")))
}

// Syncable pitch statuses: approved, or a previous attempt needing reconciliation.
var syncablePitch = map[string]bool{"ready": true, "error": true, "uncertain": true, "failed": true}

func (r *run) syncPitches(ids []int64, conn *Conn) {
	if r.cfg.InboundSheet == "" || r.cfg.InboundTab == "" {
		r.counters["fatal"] = "Set the inbound sheet ID and tab in Settings before syncing. Nothing was written."
		r.audit("sync_error", r.counters["fatal"].(string), nil)
		return
	}
	lq := store.ListQuery{PerPage: 500, IDs: ids, Sort: "received"}
	if len(ids) == 0 {
		lq.Status = "ready"
	}
	pitches, _, err := r.env.Store.Pitches(r.ctx, lq)
	if err != nil {
		r.counters["fatal"] = "Could not read saved pitches"
		return
	}
	var todo []store.Pitch
	for _, p := range pitches {
		if syncablePitch[p.Status] || (len(ids) > 0 && p.Status == "conflict") {
			todo = append(todo, p)
		} else if len(ids) > 0 {
			r.inc("not_ready", 1)
		}
	}
	if len(todo) == 0 {
		return
	}
	snapshot, err := conn.Sheets.Values(r.ctx, r.cfg.InboundSheet, google.FullRange(r.cfg.InboundTab))
	if err == nil {
		err = google.ValidateInbound(snapshot)
	}
	if err != nil {
		r.counters["fatal"] = netx.SafeMessage(err)
		r.audit("sync_error", "Inbound sheet: "+netx.SafeMessage(err), nil)
		return
	}
	for i, p := range todo {
		if r.cancelled() {
			return
		}
		r.save(70+i*29/len(todo), fmt.Sprintf("Writing and verifying %s (%d of %d)", p.Name, i+1, len(todo)))
		var data extract.Pitch
		_ = json.Unmarshal(p.Data, &data)
		if r.cfg.Mode == "live" && (data.Demo || p.Mode == "demo") {
			_ = r.env.Store.SetPitchStatus(r.ctx, p.ID, "error", "Demo pitch cannot be written to a live sheet")
			r.inc("sync_failed", 1)
			continue
		}
		if strings.TrimSpace(data.Name) == "" || len(strings.TrimSpace(data.Description)) < 10 {
			_ = r.env.Store.SetPitchStatus(r.ctx, p.ID, "review", "Name and description are required before syncing")
			r.inc("sync_failed", 1)
			continue
		}
		row := google.InboundRow(data.Name, data.Description, data.HQ, data.Contact, data.Notes)
		prior, hasPrior := r.env.Store.LatestSyncOp(r.ctx, "pitch", p.ID)
		op, err := r.env.Store.BeginSyncOp(r.ctx, "pitch", p.ID, r.cfg.InboundSheet+"/"+r.cfg.InboundTab, row, r.job.ID)
		if err != nil {
			r.inc("sync_failed", 1)
			continue
		}
		_ = r.env.Store.SetPitchStatus(r.ctx, p.ID, "syncing", "")
		res := google.SyncRow(r.ctx, conn.Sheets, r.cfg.InboundSheet, r.cfg.InboundTab, snapshot, row,
			google.InboundMatcher(domain.NameKey(data.Name), data.Website), hasPrior && (prior.Status == "uncertain" || prior.Status == "conflict"),
			func() error { return r.env.Store.MarkSyncOp(r.ctx, op.ID, "writing", 0, "", "", true) })
		r.applyPitchResult(p, data, op, res, &snapshot, row)
	}
}

func (r *run) applyPitchResult(p store.Pitch, data extract.Pitch, op store.SyncOp, res google.Result, snapshot *[][]string, row []string) {
	errMsg := ""
	if res.Err != nil {
		errMsg = netx.SafeMessage(res.Err)
	}
	_ = r.env.Store.MarkSyncOp(r.ctx, op.ID, res.Status, res.Row, res.Range, errMsg, false)
	status := res.Status
	switch res.Status {
	case "verified", "already_exists":
		data.SheetRow, data.SheetRange = int(res.Row), res.Range
		b, _ := json.Marshal(data)
		p.Data, p.Status, p.Error = b, res.Status, ""
		_ = r.env.Store.UpdatePitch(r.ctx, p)
		r.inc(res.Status, 1)
		if res.Status == "verified" && res.Row > int64(len(*snapshot)) {
			for int64(len(*snapshot)) < res.Row-1 {
				*snapshot = append(*snapshot, []string{})
			}
			*snapshot = append(*snapshot, row)
		}
		r.audit("sync", fmt.Sprintf("%s: %s · sheet row %d", p.Name, strings.ReplaceAll(res.Status, "_", " "), res.Row), map[string]any{"pitch_id": p.ID, "row": res.Row, "range": res.Range})
		return
	case "failed":
		status = "error"
	}
	_ = r.env.Store.SetPitchStatus(r.ctx, p.ID, status, errMsg)
	r.inc("sync_failed", 1)
	r.audit("sync_error", p.Name+": "+errMsg, map[string]any{"pitch_id": p.ID, "status": res.Status})
}
