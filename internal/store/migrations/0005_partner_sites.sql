-- v0.5.0: where a partner's website came from (sheet, manual, email domain,
-- checked guess, or opt-in search), a note for reviewers, the partner-list
-- contact used as a hint, and when a missing website was last looked up.
ALTER TABLE partners ADD COLUMN website_source TEXT NOT NULL DEFAULT '';
ALTER TABLE partners ADD COLUMN website_note TEXT NOT NULL DEFAULT '';
ALTER TABLE partners ADD COLUMN contact_hint TEXT NOT NULL DEFAULT '';
ALTER TABLE partners ADD COLUMN site_checked_at TEXT NOT NULL DEFAULT '';
