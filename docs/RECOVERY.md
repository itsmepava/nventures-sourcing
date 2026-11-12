# Recovery verification — 11 November 2026

Recovered this project from literal source edits stored in the supplied Codex chat backup. The original archive remains unchanged. Shell commands from the archive were inspected as data; live sourcing, email actions, and historical external writes were not replayed.

Recovered components include the React/TypeScript/Tailwind interface, dark/light/system themes, preferences, search, pagination, watchlist, FastAPI backend, outbound sourcing engine, Gmail intake, Google Sheets verification, database models and all three migrations, Windows/Linux launchers, OAuth connection helper, tests and documentation. The supplied logo, icon and Singapore image were recovered from embedded image attachments.

123 file patch operations were replayed. Six historical context conflicts were reviewed: redundant launcher/setup edits and documentation changes already represented in nearby revisions, plus the inbox router insertion whose surrounding comment differed. The inbox router was restored before the static frontend mount. Access logging is disabled in the single-process launcher so OAuth callback URLs are not written to access logs. The missing frontend dependency lockfile was regenerated from the recovered package manifest; the original lockfile and compiled bundle were not present in the archive.

Verified on this Linux installation:

- All 27 backend regression tests passed.
- All 4 frontend tests passed.
- TypeScript checking and production frontend build passed.
- Linux start.sh launched the API and compiled interface through one Python process.
- Localhost smoke checks passed for reads, profiles, export, validation, duplicate/concurrent run prevention, cancellation, audit, and blocking sheet writes in demo mode.
- Gmail demo intake produced two fictional pitches, exercising the ready and review paths without Google requests.
- Dashboard, pitch inbox, original branding and dark mode were checked in the browser. UI assets returned HTTP 200.

Recovery limits: this archive contained no nVentures application database or project .env file. The running copy has a new demo database and blank provider credentials. Prior local company records and run history have not been restored. Google Sheets data is stored separately and has not been changed or verified during recovery. Live Google, Tavily and OpenRouter integration calls await credentials and explicit live configuration. Linked decks and attachments still require manual review as documented in INBOX.md.

The project's .venv was created with the Python runtime available in Codex because system Python lacked ensurepip. It is ready on this computer; daily startup uses sh start.sh and requires no Node.js. Dependencies and development tools are excluded from the portable backup.
