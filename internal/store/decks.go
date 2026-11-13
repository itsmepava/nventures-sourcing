package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// PitchDeck is the text read from one pitch attachment, on request.
type PitchDeck struct {
	ID         int64           `json:"id"`
	PitchID    int64           `json:"pitch_id"`
	PartID     string          `json:"part_id"`
	Filename   string          `json:"filename"`
	MimeType   string          `json:"mime_type"`
	Size       int64           `json:"size"`
	SHA256     string          `json:"sha256"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Note       string          `json:"note"`
	PageCount  int             `json:"page_count"`
	Truncated  bool            `json:"truncated"`
	Pages      json.RawMessage `json:"pages"`
	Facts      json.RawMessage `json:"facts"`
	ReadBy     *int64          `json:"read_by"`
	ReadByName string          `json:"read_by_name,omitempty"`
	ReadAt     string          `json:"read_at"`
}

// SaveDeck stores (or replaces) the reading of one attachment.
func (s *Store) SaveDeck(ctx context.Context, d PitchDeck) (PitchDeck, error) {
	d.ReadAt = Now()
	if len(d.Pages) == 0 {
		d.Pages = json.RawMessage("[]")
	}
	if len(d.Facts) == 0 {
		d.Facts = json.RawMessage("[]")
	}
	var by any
	if d.ReadBy != nil {
		by = *d.ReadBy
	}
	err := s.DB.QueryRowContext(ctx, `INSERT INTO pitch_decks(pitch_id,part_id,filename,mime_type,size,sha256,kind,status,note,page_count,truncated,pages,facts,read_by,read_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(pitch_id,part_id,filename) DO UPDATE SET mime_type=excluded.mime_type,size=excluded.size,sha256=excluded.sha256,kind=excluded.kind,status=excluded.status,
note=excluded.note,page_count=excluded.page_count,truncated=excluded.truncated,pages=excluded.pages,facts=excluded.facts,read_by=excluded.read_by,read_at=excluded.read_at
RETURNING id`, d.PitchID, d.PartID, d.Filename, d.MimeType, d.Size, d.SHA256, d.Kind, d.Status, d.Note, d.PageCount, d.Truncated, string(d.Pages), string(d.Facts), by, d.ReadAt).Scan(&d.ID)
	return d, err
}

// Decks lists the readings stored for a pitch.
func (s *Store) Decks(ctx context.Context, pitchID int64) ([]PitchDeck, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,d.pitch_id,d.part_id,d.filename,d.mime_type,d.size,d.sha256,d.kind,d.status,d.note,d.page_count,d.truncated,d.pages,d.facts,d.read_by,COALESCE(u.name,''),d.read_at
FROM pitch_decks d LEFT JOIN users u ON u.id=d.read_by WHERE d.pitch_id=? ORDER BY d.id`, pitchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PitchDeck{}
	for rows.Next() {
		var d PitchDeck
		var pages, facts string
		var by sql.NullInt64
		if err := rows.Scan(&d.ID, &d.PitchID, &d.PartID, &d.Filename, &d.MimeType, &d.Size, &d.SHA256, &d.Kind, &d.Status, &d.Note, &d.PageCount, &d.Truncated, &pages, &facts, &by, &d.ReadByName, &d.ReadAt); err != nil {
			return nil, err
		}
		d.Pages, d.Facts = json.RawMessage(pages), json.RawMessage(facts)
		if by.Valid {
			d.ReadBy = &by.Int64
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
