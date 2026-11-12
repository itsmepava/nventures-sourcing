package jobs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/itsmepava/nventures-sourcing/internal/discovery"
	"github.com/itsmepava/nventures-sourcing/internal/domain"
	"github.com/itsmepava/nventures-sourcing/internal/google"
	"github.com/itsmepava/nventures-sourcing/internal/netx"
	"github.com/itsmepava/nventures-sourcing/internal/store"
)

type outboundSheet struct {
	conn     *Conn
	snapshot [][]string
}

func (r *run) localIdentities() []domain.Identity {
	keys, _ := r.env.Store.CompanyKeys(r.ctx)
	out := make([]domain.Identity, 0, len(keys))
	for _, k := range keys {
		out = append(out, domain.Identity{NameKey: k[0], Domain: k[1], LinkedInKey: k[2]})
	}
	return out
}

// openOutbound reads the outbound sheet and imports partners/control state.
func (r *run) openOutbound(required bool) (*outboundSheet, []domain.Identity, error) {
	if r.cfg.OutboundSheet == "" {
		if required {
			return nil, nil, &netx.ProviderError{Msg: "Set the outbound sheet ID in Settings before syncing. Nothing was written."}
		}
		return nil, nil, nil
	}
	conn, err := r.connectGoogle()
	if err != nil {
		if required {
			return nil, nil, err
		}
		r.audit("warning", "Outbound sheet not read: "+netx.SafeMessage(err)+". Duplicates are checked against local records only.", nil)
		return nil, nil, nil
	}
	snap, err := conn.Sheets.Values(r.ctx, r.cfg.OutboundSheet, google.FullRange(r.cfg.OutboundTab))
	if err == nil {
		err = google.ValidateOutbound(snap)
	}
	if err != nil {
		if required {
			return nil, nil, err
		}
		r.audit("warning", "Outbound sheet not read: "+netx.SafeMessage(err), nil)
		return nil, nil, nil
	}
	var remote []domain.Identity
	headers := snap[0]
	col := func(row []string, name string) string {
		for i, h := range headers {
			if google.Norm(h) == name && i < len(row) {
				return row[i]
			}
		}
		return ""
	}
	for _, row := range snap[1:] {
		remote = append(remote, domain.IdentityOf(col(row, "company name"), col(row, "website"), col(row, "company linkedin")))
	}
	r.importPartners(conn)
	return &outboundSheet{conn: conn, snapshot: snap}, remote, nil
}

func (r *run) importPartners(conn *Conn) {
	if r.cfg.PartnerTab == "" {
		return
	}
	rows, err := conn.Sheets.Values(r.ctx, r.cfg.OutboundSheet, google.FullRange(r.cfg.PartnerTab))
	if err != nil || len(rows) == 0 {
		if err != nil {
			r.audit("warning", "Partner list not read: "+netx.SafeMessage(err), nil)
		}
		return
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[google.Norm(h)] = i
	}
	cell := func(row []string, k string) string {
		if i, ok := idx[k]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	if _, ok := idx["company name"]; !ok {
		r.audit("warning", "Partner list needs a Company Name header; partners not imported", nil)
		return
	}
	state := map[string][]string{}
	if r.cfg.ControlTab != "" {
		if ctl, err := conn.Sheets.Values(r.ctx, r.cfg.OutboundSheet, google.FullRange(r.cfg.ControlTab)); err == nil {
			for _, row := range ctl[min(1, len(ctl)):] {
				if len(row) > 0 {
					state[google.Norm(row[0])] = row
				}
			}
		}
	}
	n := 0
	for _, row := range rows[1:] {
		name := cell(row, "company name")
		if name == "" {
			continue
		}
		st := append(state[google.Norm(name)], "", "", "", "")
		runs, _ := strconv.Atoi(st[2])
		if err := r.env.Store.MergePartner(r.ctx, name, cell(row, "website"), !strings.EqualFold(cell(row, "nventures relevance"), "no"), st[1], runs); err == nil {
			n++
		}
	}
	r.counters["partners_imported"] = n
}

func (r *run) updateControl(sheet *outboundSheet, p store.Partner) {
	if sheet == nil || r.cfg.ControlTab == "" {
		return
	}
	rows, err := sheet.conn.Sheets.Values(r.ctx, r.cfg.OutboundSheet, google.FullRange(r.cfg.ControlTab))
	if err != nil || len(rows) == 0 {
		return
	}
	want := []string{"partner name", "last sourced utc", "run count", "last run added"}
	for i, w := range want {
		if i >= len(rows[0]) || google.Norm(rows[0][i]) != w {
			r.audit("warning", "Control tab needs Partner Name / Last Sourced UTC / Run Count / Last Run Added headers; not updated", nil)
			return
		}
	}
	fresh, _ := r.env.Store.Partner(r.ctx, p.ID)
	row := []string{fresh.Name, fresh.LastSourced, strconv.Itoa(fresh.RunCount), strconv.Itoa(fresh.LastAdded)}
	for i, old := range rows[1:] {
		if len(old) > 0 && google.Norm(old[0]) == google.Norm(fresh.Name) {
			n := i + 2
			if err := sheet.conn.Sheets.Update(r.ctx, r.cfg.OutboundSheet, fmt.Sprintf("%s!A%d:D%d", google.QuoteTab(r.cfg.ControlTab), n, n), row); err != nil {
				r.audit("warning", "Partner control update: "+netx.SafeMessage(err), nil)
			}
			return
		}
	}
	if _, err := sheet.conn.Sheets.Append(r.ctx, r.cfg.OutboundSheet, r.cfg.ControlTab, row); err != nil {
		r.audit("warning", "Partner control append: "+netx.SafeMessage(err), nil)
	}
}

func (r *run) stopOutbound(target, maxResearch int) bool {
	return r.cancelled() || r.get("accepted") >= target || r.get("researched") >= maxResearch
}

func (r *run) outboundRun(p map[string]any) {
	target := intParam(p, "target", 10, 1, 100)
	maxPartners := intParam(p, "max_partners", 6, 1, 30)
	syncNow := boolParam(p, "sync")
	recheck := boolParam(p, "recheck")
	maxResearch := r.cfg.MaxResearch
	if maxResearch <= 0 {
		maxResearch = 45
	}
	r.counters["target"] = target
	sheet, remote, err := r.openOutbound(syncNow)
	if err != nil {
		r.fail(err)
		return
	}
	partners, err := r.env.Store.Partners(r.ctx, true, maxPartners)
	if err != nil || len(partners) == 0 {
		r.finish("failed", "No relevant partners. Add a partner with a website, or connect the partner sheet.")
		return
	}
	seen := append(r.localIdentities(), remote...)
	for pi, partner := range partners {
		if r.stopOutbound(target, maxResearch) {
			break
		}
		r.save(min(95, pi*90/len(partners)), "Crawling "+partner.Name)
		added := 0
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					r.inc("errors", 1)
					r.audit("error", partner.Name+": internal error; continuing with the next partner", nil)
				}
			}()
			rep := r.env.Crawler.DiscoverPartner(r.ctx, partner.Name, partner.Website)
			r.inc("pages_visited", rep.PagesVisited)
			r.inc("candidates_found", len(rep.Candidates))
			r.audit("partner", fmt.Sprintf("%s: %d pages, %d company links%s", partner.Name, rep.PagesVisited, len(rep.Candidates), warnSuffix(rep.Warnings)), rep)
			if !rep.Complete {
				r.inc("incomplete_partners", 1)
			}
			for _, c := range rep.Candidates {
				if r.stopOutbound(target, maxResearch) {
					break
				}
				if r.evaluate(c, &seen, sheet, syncNow, recheck) {
					added++
				}
			}
			summary := map[string]any{"pages_visited": rep.PagesVisited, "candidates": len(rep.Candidates), "warnings": rep.Warnings, "complete": rep.Complete, "added": added, "job_id": r.job.ID}
			_ = r.env.Store.MarkPartnerSourced(r.ctx, partner.ID, added, summary)
		}()
		if syncNow {
			r.updateControl(sheet, partner)
		}
	}
	status := "partial"
	switch {
	case r.cancelled():
		status = "cancelled"
	case r.get("accepted") >= target:
		status = "completed"
		if r.get("sync_failed") > 0 || r.get("errors") > 0 {
			status = "completed_with_errors"
		}
	}
	msg := fmt.Sprintf("%d/%d accepted · %d rejected · %d need research · %d duplicates · %d errors", r.get("accepted"), target, r.get("rejected"), r.get("needs_research"), r.get("duplicates"), r.get("errors"))
	if syncNow {
		msg += fmt.Sprintf(" · %d verified sheet rows · %d sync failures", r.get("verified"), r.get("sync_failed"))
	}
	if status == "partial" {
		msg += " · target not reached: no further eligible companies were evidenced (qualification was not relaxed)"
	}
	r.finish(status, msg)
}

func warnSuffix(w []string) string {
	if len(w) == 0 {
		return ""
	}
	return fmt.Sprintf(" · %d warning(s)", len(w))
}

// evaluate researches one candidate and records the outcome. It returns true
// when a new company was accepted.
func (r *run) evaluate(c discovery.Candidate, seen *[]domain.Identity, sheet *outboundSheet, syncNow, recheck bool) bool {
	jobID := r.job.ID
	id := domain.IdentityOf(c.Name, c.URL, "")
	if domain.Duplicate(id, *seen) {
		r.inc("duplicates", 1)
		_, _ = r.env.Store.AddCandidate(r.ctx, store.Candidate{JobID: &jobID, Partner: c.Partner, Name: c.Name, URL: c.URL, Outcome: "duplicate", Reasons: []string{"Already sourced (name or website)"}})
		r.audit("duplicate", c.Name+": already sourced", nil)
		return false
	}
	if !recheck {
		if o, ok := r.env.Store.KnownCandidateURL(r.ctx, c.URL); ok && o == "rejected" {
			r.inc("previously_rejected", 1)
			return false
		}
	}
	r.save(r.progress, "Researching "+c.Name+" · "+c.Partner)
	rec, notes := r.env.Crawler.ResearchCompany(r.ctx, c.Name, c.URL)
	if r.ctx.Err() != nil {
		return false
	}
	r.inc("researched", 1)
	rec.Investors = c.Partner
	rec.Demo = r.cfg.Mode == "demo"
	if rec.Name == "" {
		rec.Name = c.Name
	}
	reasons := domain.Qualifies(rec)
	recJSON, _ := json.Marshal(rec)
	srcJSON, _ := json.Marshal(notes)
	cand := store.Candidate{JobID: &jobID, Partner: c.Partner, Name: rec.Name, URL: c.URL, Reasons: reasons, Record: recJSON, Sources: srcJSON}
	full := domain.IdentityOf(rec.Name, rec.Website, rec.LinkedIn)
	switch {
	case len(reasons) > 0 && domain.Contradiction(reasons):
		cand.Outcome = "rejected"
		r.inc("rejected", 1)
		r.audit("rejected", rec.Name+": "+strings.Join(reasons, "; "), map[string]any{"reasons": reasons, "url": c.URL})
	case len(reasons) > 0:
		cand.Outcome = "needs_research"
		r.inc("needs_research", 1)
		r.audit("needs_research", rec.Name+": "+strings.Join(reasons, "; "), map[string]any{"reasons": reasons, "url": c.URL})
	case domain.Duplicate(full, *seen):
		cand.Outcome = "duplicate"
		r.inc("duplicates", 1)
		r.audit("duplicate", rec.Name+": matching website or LinkedIn", nil)
	default:
		syncStatus := "pending"
		comp, err := r.env.Store.InsertCompany(r.ctx, store.Company{Name: rec.Name, NameKey: full.NameKey, Domain: full.Domain, LinkedInKey: full.LinkedInKey, Data: recJSON, Partner: c.Partner, SyncStatus: syncStatus, Demo: rec.Demo, JobID: &jobID})
		if err == store.ErrDuplicate {
			cand.Outcome = "duplicate"
			r.inc("duplicates", 1)
			r.audit("duplicate", rec.Name+": unique constraint prevented a duplicate", nil)
			break
		}
		if err != nil {
			r.inc("errors", 1)
			r.audit("error", rec.Name+": could not save", nil)
			return false
		}
		cand.Outcome, cand.CompanyID = "accepted", &comp.ID
		*seen = append(*seen, full)
		r.inc("accepted", 1)
		r.audit("accepted", rec.Name+": accepted with evidence and saved locally", map[string]any{"company_id": comp.ID})
		_, _ = r.env.Store.AddCandidate(r.ctx, cand)
		if syncNow && sheet != nil {
			r.save(r.progress, "Writing and verifying "+comp.Name+" in the outbound sheet")
			r.syncCompany(comp, sheet)
		}
		return true
	}
	_, _ = r.env.Store.AddCandidate(r.ctx, cand)
	return false
}

func (r *run) syncCompany(c store.Company, sheet *outboundSheet) {
	var rec domain.Record
	_ = json.Unmarshal(c.Data, &rec)
	if reasons := domain.Qualifies(rec); len(reasons) > 0 {
		_ = r.env.Store.SetCompanySync(r.ctx, c.ID, "blocked", 0, "", strings.Join(reasons, "; "))
		r.inc("sync_failed", 1)
		return
	}
	if r.cfg.Mode == "live" && (rec.Demo || c.Demo) {
		_ = r.env.Store.SetCompanySync(r.ctx, c.ID, "blocked", 0, "", "Demo company cannot sync to a live sheet")
		r.inc("sync_failed", 1)
		return
	}
	row := google.OutboundRow(sheet.snapshot[0], c.Data, c.Partner)
	prior, hasPrior := r.env.Store.LatestSyncOp(r.ctx, "company", c.ID)
	op, err := r.env.Store.BeginSyncOp(r.ctx, "company", c.ID, r.cfg.OutboundSheet+"/"+r.cfg.OutboundTab, row, r.job.ID)
	if err != nil {
		r.inc("sync_failed", 1)
		return
	}
	_ = r.env.Store.SetCompanySync(r.ctx, c.ID, "syncing", 0, "", "")
	res := google.SyncRow(r.ctx, sheet.conn.Sheets, r.cfg.OutboundSheet, r.cfg.OutboundTab, sheet.snapshot, row,
		google.OutboundMatcher(domain.Identity{NameKey: c.NameKey, Domain: c.Domain, LinkedInKey: c.LinkedInKey}), hasPrior && (prior.Status == "uncertain" || prior.Status == "conflict"),
		func() error { return r.env.Store.MarkSyncOp(r.ctx, op.ID, "writing", 0, "", "", true) })
	errMsg := ""
	if res.Err != nil {
		errMsg = netx.SafeMessage(res.Err)
	}
	_ = r.env.Store.MarkSyncOp(r.ctx, op.ID, res.Status, res.Row, res.Range, errMsg, false)
	status := res.Status
	if status == "failed" {
		status = "error"
	}
	_ = r.env.Store.SetCompanySync(r.ctx, c.ID, status, res.Row, res.Range, errMsg)
	switch res.Status {
	case "verified", "already_exists":
		r.inc(res.Status, 1)
		if res.Status == "verified" && res.Row > int64(len(sheet.snapshot)) {
			for int64(len(sheet.snapshot)) < res.Row-1 {
				sheet.snapshot = append(sheet.snapshot, []string{})
			}
			sheet.snapshot = append(sheet.snapshot, row)
		}
		r.audit("sync", fmt.Sprintf("%s: %s · sheet row %d", c.Name, strings.ReplaceAll(res.Status, "_", " "), res.Row), map[string]any{"company_id": c.ID, "row": res.Row, "range": res.Range})
	default:
		r.inc("sync_failed", 1)
		r.audit("sync_error", c.Name+": "+errMsg, map[string]any{"company_id": c.ID, "status": res.Status})
	}
}

var syncableCompany = map[string]bool{"pending": true, "error": true, "uncertain": true}

func (r *run) outboundSyncJob(p map[string]any) {
	sheet, _, err := r.openOutbound(true)
	if err != nil {
		r.fail(err)
		return
	}
	ids := idsParam(p)
	var comps []store.Company
	if len(ids) > 0 {
		for _, id := range ids {
			if c, err := r.env.Store.Company(r.ctx, id); err == nil {
				comps = append(comps, c)
			}
		}
	} else {
		comps, _ = r.env.Store.AllCompanies(r.ctx, "sync_status IN ('pending','error','uncertain')")
	}
	for i, c := range comps {
		if r.cancelled() {
			break
		}
		if !syncableCompany[c.SyncStatus] && !(len(ids) > 0 && c.SyncStatus == "conflict") {
			r.inc("not_pending", 1)
			continue
		}
		r.save(i*100/max(len(comps), 1), fmt.Sprintf("Writing and verifying %s (%d of %d)", c.Name, i+1, len(comps)))
		r.syncCompany(c, sheet)
	}
	status := "completed"
	if r.cancelled() {
		status = "cancelled"
	} else if r.get("sync_failed") > 0 {
		status = "completed_with_errors"
	}
	r.finish(status, fmt.Sprintf("%d verified sheet rows · %d already in sheet · %d need attention", r.get("verified"), r.get("already_exists"), r.get("sync_failed")))
}

// outboundResearch researches manually supplied company URLs through the
// same deterministic qualification path. Useful when a portfolio is
// JavaScript-only or blocked.
func (r *run) outboundResearch(p map[string]any) {
	urls := stringsParam(p, "urls")
	partner, _ := p["partner"].(string)
	if partner == "" {
		partner = "Manual research"
	}
	if len(urls) == 0 {
		r.finish("failed", "No company URLs supplied")
		return
	}
	seen := r.localIdentities()
	for i, u := range urls {
		if r.cancelled() {
			break
		}
		if !strings.Contains(u, "://") {
			u = "https://" + u
		}
		if _, err := r.env.Crawler.F.ValidateURL(u); err != nil {
			r.inc("errors", 1)
			r.audit("error", u+": refused ("+err.Error()+")", nil)
			continue
		}
		r.save(i*100/len(urls), "Researching "+u)
		r.evaluate(discovery.Candidate{Name: "", URL: u, Partner: partner, FoundOn: "manual"}, &seen, nil, false, true)
	}
	status := "completed"
	if r.cancelled() {
		status = "cancelled"
	}
	r.finish(status, fmt.Sprintf("%d accepted · %d rejected · %d need research · %d duplicates · %d errors", r.get("accepted"), r.get("rejected"), r.get("needs_research"), r.get("duplicates"), r.get("errors")))
}
