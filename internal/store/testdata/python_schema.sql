CREATE TABLE alembic_version (
	version_num VARCHAR(32) NOT NULL, 
	CONSTRAINT alembic_version_pkc PRIMARY KEY (version_num)
);
CREATE TABLE companies (
	id INTEGER NOT NULL, 
	name VARCHAR(200) NOT NULL, 
	name_key VARCHAR(200) NOT NULL, 
	domain VARCHAR(300), 
	linkedin_key VARCHAR(300), 
	data JSON NOT NULL, 
	partner VARCHAR(200) NOT NULL, 
	created_at VARCHAR(50) NOT NULL, 
	sync_status VARCHAR(40) NOT NULL, 
	sheet_row INTEGER, 
	sheet_range VARCHAR(300), 
	sync_error TEXT, 
	PRIMARY KEY (id), 
	UNIQUE (name_key), 
	UNIQUE (domain), 
	UNIQUE (linkedin_key)
);
CREATE TABLE partners (
	id INTEGER NOT NULL, 
	name VARCHAR(200) NOT NULL, 
	website VARCHAR(500) NOT NULL, 
	relevant INTEGER NOT NULL, 
	last_sourced VARCHAR(50) NOT NULL, 
	run_count INTEGER NOT NULL, 
	last_added INTEGER NOT NULL, 
	PRIMARY KEY (id), 
	UNIQUE (name)
);
CREATE TABLE runs (
	id INTEGER NOT NULL, 
	started_at VARCHAR(50) NOT NULL, 
	finished_at VARCHAR(50), 
	mode VARCHAR(20) NOT NULL, 
	status VARCHAR(30) NOT NULL, 
	target INTEGER NOT NULL, 
	max_partners INTEGER NOT NULL, 
	accepted INTEGER NOT NULL, 
	rejected INTEGER NOT NULL, 
	duplicates INTEGER NOT NULL, 
	errors INTEGER NOT NULL, 
	researched INTEGER NOT NULL, 
	progress INTEGER NOT NULL, 
	cancel_requested INTEGER NOT NULL, 
	message TEXT NOT NULL, sync_to_sheets INTEGER DEFAULT '0' NOT NULL, synced INTEGER DEFAULT '0' NOT NULL, sync_errors INTEGER DEFAULT '0' NOT NULL, 
	PRIMARY KEY (id)
);
CREATE TABLE events (
	id INTEGER NOT NULL, 
	run_id INTEGER, 
	created_at VARCHAR(50) NOT NULL, 
	kind VARCHAR(40) NOT NULL, 
	message TEXT NOT NULL, 
	detail JSON NOT NULL, 
	PRIMARY KEY (id), 
	FOREIGN KEY(run_id) REFERENCES runs (id)
);
CREATE TABLE pitches (
	id INTEGER NOT NULL, 
	message_id VARCHAR(200) NOT NULL, 
	mode VARCHAR(20) NOT NULL, 
	name VARCHAR(300) NOT NULL, 
	status VARCHAR(40) NOT NULL, 
	data JSON NOT NULL, 
	created_at VARCHAR(50) NOT NULL, 
	error TEXT NOT NULL, 
	PRIMARY KEY (id), 
	UNIQUE (message_id)
);
CREATE TABLE inbox_jobs (
	id INTEGER NOT NULL, 
	status VARCHAR(40) NOT NULL, 
	data JSON NOT NULL, 
	created_at VARCHAR(50) NOT NULL, 
	PRIMARY KEY (id)
);
