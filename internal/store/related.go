package store

import (
	"context"
	"strings"
)

// Related collects records across modules that share a company identity
// (normalised name or non-shared website domain). It never merges records;
// it only shows the relationships so analysts see one company consistently.
type Related struct {
	Companies  []Company   `json:"companies"`
	Pitches    []Pitch     `json:"pitches"`
	Candidates []Candidate `json:"candidates"`
	Events     []Event     `json:"events"`
}

// RelatedTo finds records matching nameKey or domain. Excluded IDs are the
// record being viewed.
func (s *Store) RelatedTo(ctx context.Context, nameKey, domain string, excludeCompany, excludePitch int64) (Related, error) {
	out := Related{Companies: []Company{}, Pitches: []Pitch{}, Candidates: []Candidate{}, Events: []Event{}}
	if nameKey == "" && domain == "" {
		return out, nil
	}
	comps, err := s.AllCompanies(ctx, "id<>? AND ((name_key<>'' AND name_key=?) OR (? <> '' AND domain=?))", excludeCompany, nameKey, domain, domain)
	if err != nil {
		return out, err
	}
	out.Companies = comps
	rows, err := s.DB.QueryContext(ctx, `SELECT `+pitchCols+` FROM pitches WHERE id<>? AND ((name_key<>'' AND name_key=?) OR (domain<>'' AND domain=?)) ORDER BY id DESC LIMIT 50`, excludePitch, nameKey, domain)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		p, err := scanPitch(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Pitches = append(out.Pitches, p)
	}
	rows.Close()
	var candArgs []any
	q := `SELECT ` + candCols + ` FROM candidates WHERE 0`
	if domain != "" {
		q += ` OR url LIKE ? OR url LIKE ?`
		candArgs = append(candArgs, "%://"+domain+"/%", "%://www."+domain+"/%")
	}
	q += ` ORDER BY id DESC LIMIT 50`
	crow, err := s.DB.QueryContext(ctx, q, candArgs...)
	if err != nil {
		return out, err
	}
	for crow.Next() {
		c, err := scanCand(crow)
		if err != nil {
			crow.Close()
			return out, err
		}
		out.Candidates = append(out.Candidates, c)
	}
	crow.Close()
	// Activity: events that reference any related company or pitch by ID.
	var ids []string
	var args []any
	for _, c := range append([]Company{}, out.Companies...) {
		ids = append(ids, "(json_extract(detail,'$.company_id')=?)")
		args = append(args, c.ID)
	}
	if excludeCompany > 0 {
		ids = append(ids, "(json_extract(detail,'$.company_id')=?)")
		args = append(args, excludeCompany)
	}
	for _, p := range out.Pitches {
		ids = append(ids, "(json_extract(detail,'$.pitch_id')=?)")
		args = append(args, p.ID)
	}
	if excludePitch > 0 {
		ids = append(ids, "(json_extract(detail,'$.pitch_id')=?)")
		args = append(args, excludePitch)
	}
	if len(ids) > 0 {
		evs, err := s.eventsWhere(ctx, strings.Join(ids, " OR "), args...)
		if err != nil {
			return out, err
		}
		out.Events = evs
	}
	return out, nil
}

func (s *Store) eventsWhere(ctx context.Context, where string, args ...any) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, job_id, created_at, kind, message, detail FROM events WHERE `+where+` ORDER BY id DESC LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var jid *int64
		var d string
		if err := rows.Scan(&e.ID, &jid, &e.CreatedAt, &e.Kind, &e.Message, &d); err != nil {
			return nil, err
		}
		e.JobID, e.Detail = jid, []byte(d)
		out = append(out, e)
	}
	return out, rows.Err()
}
