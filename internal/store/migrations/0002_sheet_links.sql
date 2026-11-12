-- v0.3.0-alpha.2: store a direct link to the verified sheet row.
ALTER TABLE companies ADD COLUMN sheet_url TEXT NOT NULL DEFAULT '';
ALTER TABLE pitches ADD COLUMN sheet_url TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS companies_domain_lookup ON companies(domain);
