-- v0.4.0: multi-user workspace — accounts, roles, sessions, assignments,
-- notes, reply templates, schedules and notifications.
CREATE TABLE users (
	id INTEGER PRIMARY KEY,
	email TEXT NOT NULL UNIQUE COLLATE NOCASE,
	name TEXT NOT NULL,
	role TEXT NOT NULL CHECK (role IN ('admin','sourcer','responder','viewer')),
	password_hash TEXT NOT NULL,
	active INTEGER NOT NULL DEFAULT 1,
	must_change_password INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	last_login_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	last_seen_at TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

ALTER TABLE events ADD COLUMN user_id INTEGER;
ALTER TABLE events ADD COLUMN actor TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN created_by INTEGER;
ALTER TABLE jobs ADD COLUMN created_by_name TEXT NOT NULL DEFAULT '';

ALTER TABLE pitches ADD COLUMN assignee_id INTEGER;
ALTER TABLE pitches ADD COLUMN stage TEXT NOT NULL DEFAULT 'new';
ALTER TABLE pitches ADD COLUMN follow_up_at TEXT NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN assignee_id INTEGER;
ALTER TABLE companies ADD COLUMN stage TEXT NOT NULL DEFAULT 'not_contacted';
ALTER TABLE companies ADD COLUMN follow_up_at TEXT NOT NULL DEFAULT '';
CREATE INDEX pitches_assignee ON pitches(assignee_id);
CREATE INDEX companies_assignee ON companies(assignee_id);

CREATE TABLE notes (
	id INTEGER PRIMARY KEY,
	entity TEXT NOT NULL CHECK (entity IN ('pitch','company')),
	entity_id INTEGER NOT NULL,
	user_id INTEGER,
	author TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX notes_entity ON notes(entity, entity_id, id);

CREATE TABLE reply_templates (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	subject TEXT NOT NULL,
	body TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
INSERT INTO reply_templates(name,subject,body,updated_at) VALUES
('Thanks — next steps','Re: {{company}} — thank you','Hi {{founder}},

Thank you for sharing {{company}} with nVentures. We have read your submission and would like to learn more. Could you share your latest deck and a few times that suit you for a 30-minute call next week?

Best regards,
{{me}}
nVentures','2026-11-13T00:00:00Z'),
('Request deck','Re: {{company}} — deck request','Hi {{founder}},

Thanks for reaching out about {{company}}. Before we can review properly, could you send your current pitch deck (a PDF or view link is fine) and a short note on traction to date?

Best regards,
{{me}}
nVentures','2026-11-13T00:00:00Z'),
('Polite pass','Re: {{company}}','Hi {{founder}},

Thank you for considering nVentures for {{company}}. After review, it is not a fit for our current mandate, so we will not take this further right now. We appreciate you thinking of us and wish you every success.

Best regards,
{{me}}
nVentures','2026-11-13T00:00:00Z');

CREATE TABLE schedules (
	id INTEGER PRIMARY KEY,
	kind TEXT NOT NULL CHECK (kind IN ('inbound_import','outbound_run')),
	params TEXT NOT NULL DEFAULT '{}',
	every_minutes INTEGER NOT NULL CHECK (every_minutes >= 15),
	next_run_at TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	last_job_id INTEGER,
	last_status TEXT NOT NULL DEFAULT '',
	created_by INTEGER,
	created_at TEXT NOT NULL
);

CREATE TABLE notifications (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	message TEXT NOT NULL,
	link TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	read_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX notifications_user ON notifications(user_id, read_at, id);
