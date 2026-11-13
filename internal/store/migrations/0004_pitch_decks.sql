-- v0.5.0: text read from pitch-deck attachments on request (PDF/PPTX), with
-- reviewable facts. Deck text is untrusted data and never changes a pitch by itself.
CREATE TABLE pitch_decks (
	id INTEGER PRIMARY KEY,
	pitch_id INTEGER NOT NULL REFERENCES pitches(id) ON DELETE CASCADE,
	part_id TEXT NOT NULL DEFAULT '',
	filename TEXT NOT NULL,
	mime_type TEXT NOT NULL DEFAULT '',
	size INTEGER NOT NULL DEFAULT 0,
	sha256 TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT '',
	page_count INTEGER NOT NULL DEFAULT 0,
	truncated INTEGER NOT NULL DEFAULT 0,
	pages TEXT NOT NULL DEFAULT '[]',
	facts TEXT NOT NULL DEFAULT '[]',
	read_by INTEGER,
	read_at TEXT NOT NULL,
	UNIQUE (pitch_id, part_id, filename)
);
CREATE INDEX pitch_decks_pitch ON pitch_decks(pitch_id);
