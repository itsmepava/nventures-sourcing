CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE partners (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	website TEXT NOT NULL DEFAULT '',
	relevant INTEGER NOT NULL DEFAULT 1,
	last_sourced TEXT NOT NULL DEFAULT '',
	run_count INTEGER NOT NULL DEFAULT 0,
	last_added INTEGER NOT NULL DEFAULT 0,
	last_report TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	legacy_ref TEXT UNIQUE
);

CREATE TABLE jobs (
	id INTEGER PRIMARY KEY,
	kind TEXT NOT NULL,
	mode TEXT NOT NULL,
	status TEXT NOT NULL,
	params TEXT NOT NULL DEFAULT '{}',
	counters TEXT NOT NULL DEFAULT '{}',
	progress INTEGER NOT NULL DEFAULT 0,
	message TEXT NOT NULL DEFAULT '',
	cancel_requested INTEGER NOT NULL DEFAULT 0,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	heartbeat_at TEXT,
	legacy_ref TEXT UNIQUE
);
CREATE INDEX jobs_kind ON jobs(kind, id);

CREATE TABLE events (
	id INTEGER PRIMARY KEY,
	job_id INTEGER REFERENCES jobs(id),
	created_at TEXT NOT NULL,
	kind TEXT NOT NULL,
	message TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '{}',
	legacy_ref TEXT UNIQUE
);
CREATE INDEX events_job ON events(job_id, id);
CREATE INDEX events_kind ON events(kind, id);

CREATE TABLE companies (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	name_key TEXT NOT NULL UNIQUE,
	domain TEXT UNIQUE,
	linkedin_key TEXT UNIQUE,
	data TEXT NOT NULL,
	partner TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	sync_status TEXT NOT NULL DEFAULT 'pending',
	sheet_row INTEGER,
	sheet_range TEXT,
	sync_error TEXT,
	starred INTEGER NOT NULL DEFAULT 0,
	demo INTEGER NOT NULL DEFAULT 0,
	job_id INTEGER,
	legacy_ref TEXT UNIQUE
);

CREATE TABLE candidates (
	id INTEGER PRIMARY KEY,
	job_id INTEGER,
	partner TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	url TEXT NOT NULL,
	outcome TEXT NOT NULL,
	reasons TEXT NOT NULL DEFAULT '[]',
	record TEXT NOT NULL DEFAULT '{}',
	sources TEXT NOT NULL DEFAULT '[]',
	company_id INTEGER,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX candidates_url ON candidates(url);
CREATE INDEX candidates_job ON candidates(job_id);

CREATE TABLE pitches (
	id INTEGER PRIMARY KEY,
	message_id TEXT NOT NULL UNIQUE,
	account TEXT NOT NULL DEFAULT '',
	thread_id TEXT NOT NULL DEFAULT '',
	mode TEXT NOT NULL,
	name TEXT NOT NULL,
	name_key TEXT NOT NULL DEFAULT '',
	domain TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	data TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	received_at TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	starred INTEGER NOT NULL DEFAULT 0,
	duplicate_of INTEGER,
	job_id INTEGER,
	legacy_ref TEXT UNIQUE
);
CREATE INDEX pitches_name_key ON pitches(name_key);
CREATE INDEX pitches_domain ON pitches(domain);
CREATE INDEX pitches_thread ON pitches(thread_id);
CREATE INDEX pitches_status ON pitches(status);

CREATE TABLE sync_ops (
	id INTEGER PRIMARY KEY,
	target TEXT NOT NULL,
	entity_id INTEGER NOT NULL,
	destination TEXT NOT NULL,
	payload TEXT NOT NULL,
	payload_hash TEXT NOT NULL,
	status TEXT NOT NULL,
	sheet_row INTEGER,
	sheet_range TEXT,
	error TEXT NOT NULL DEFAULT '',
	attempts INTEGER NOT NULL DEFAULT 0,
	job_id INTEGER,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX sync_ops_entity ON sync_ops(target, entity_id, id);
CREATE INDEX sync_ops_status ON sync_ops(status);

CREATE TABLE fetch_cache (
	url TEXT PRIMARY KEY,
	status INTEGER NOT NULL,
	content_type TEXT NOT NULL,
	etag TEXT NOT NULL DEFAULT '',
	last_modified TEXT NOT NULL DEFAULT '',
	body BLOB NOT NULL,
	truncated INTEGER NOT NULL DEFAULT 0,
	fetched_at TEXT NOT NULL
);

CREATE TABLE leases (
	name TEXT PRIMARY KEY,
	owner TEXT NOT NULL,
	job_id INTEGER,
	expires_at TEXT NOT NULL
);

CREATE TABLE mailbox_state (
	account TEXT PRIMARY KEY,
	last_scan_at TEXT NOT NULL DEFAULT '',
	more_available INTEGER NOT NULL DEFAULT 0,
	listed INTEGER NOT NULL DEFAULT 0
);
