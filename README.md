# nVentures Sourcing

## v0.5.0-alpha.1 — nVentures Workspace (current development)

One Go executable (JSON API) with the separate web frontend (`web/src`) embedded, SQLite storage, sign-in with roles (admin, sourcer, pitch responder, viewer), team assignments, notes, notifications and schedules, the inbound Gmail pitch funnel with on-request reading of PDF/PPTX pitch decks, outbound sourcing against the mandate, and verified Google Sheets sync. No Python, Node.js, Docker or paid search/AI accounts are needed.

```bash
./start-go.sh              # Linux: builds on first run, opens http://127.0.0.1:8765
./bin/nventures rehearse   # deterministic demo of both workflows; prints PASS
```

Windows: `Start nVentures (Go).cmd` next to `nventures.exe`. Demo mode works without credentials; live mode needs one Google consent step.

- **Step-by-step live setup (Gmail + Sheets):** [docs/SETUP-LIVE.md](docs/SETUP-LIVE.md)
- Guide and Google setup: [docs/GO-APP.md](docs/GO-APP.md)
- Migration from the Python app and rollback: [docs/MIGRATION-GO.md](docs/MIGRATION-GO.md)
- Frontend (separate project): [web/README.md](web/README.md)
- Five-minute presenter script: [docs/PRESENTER-SCRIPT.md](docs/PRESENTER-SCRIPT.md)
- Release notes and known limits: [0.5.0-alpha.1](docs/RELEASE-NOTES-0.5.0-alpha.1.md) · [0.4.0-alpha.1](docs/RELEASE-NOTES-0.4.0-alpha.1.md) · [0.3.0-alpha.2](docs/RELEASE-NOTES-0.3.0-alpha.2.md) · [alpha.1](docs/RELEASE-NOTES-0.3.0-alpha.1.md)
- Screenshots (light and dark): [docs/screenshots/](docs/screenshots/)
- Future Google Cloud hosting (not deployed): [docs/deploy/GOOGLE-CLOUD.md](docs/deploy/GOOGLE-CLOUD.md)
- Implementation progress: [docs/handoff/PROGRESS.md](docs/handoff/PROGRESS.md)

The Python/React app documented below (v0.2.0-alpha.1) is kept unchanged as the reference and fallback.

---

# Python/React app v0.2.0-alpha.1 (legacy reference)

A tested localhost Python/React prerelease with outbound sourcing, Gmail pitch intake, verified Google Sheets sync, branding and daily-use UI features. The Go rewrite above replaces it once live Google verification is done.

Download the attached application ZIP for the prebuilt interface, or build from source. No live credentials, private mail or databases are included.

# Workspace usability update

Light/dark/system themes, compact mode, quick actions (Ctrl/Cmd+K), saved filters, sorting, pagination, personal starred companies, pitch search and filtered export are included. See [workspace guide](docs/WORKSPACE.md).

Windows: Start.cmd (setup.ps1 once for a new installation). Linux: sh setup.sh, then sh start.sh. Read [Linux migration](docs/LINUX-MIGRATION.md) before moving existing records.

# Revamped workspace and Gmail intake

Open **Start.cmd** to run everything through Python. New users run setup.ps1 once. Open **Pitch inbox** for a one-button demo, then follow [Google connection instructions](docs/INBOX.md) to enable live imports. The outbound sourcing workflow remains available with its original mandate. No additional Python or frontend dependencies were added for inbox intake.

# nVentures Sourcing 2.0

A localhost-first sourcing workspace for nVentures Fund II. React + TypeScript + Tailwind with a local shadcn/ui Button, FastAPI, SQLite, and versioned Alembic migrations. No Streamlit, Redis, Celery, hosted auth, analytics, provider SDKs, or cloud services are needed for demo mode.

## Windows quick start

Install Python 3.11+. The downloadable build includes the compiled frontend, so Node.js is only needed for frontend development or rebuilding. Open PowerShell in this project folder:

```powershell
powershell -ExecutionPolicy Bypass -File .\setup.ps1
```

If `python` opens the Microsoft Store or cannot be found, specify your Python executable:

```powershell
powershell -ExecutionPolicy Bypass -File .\setup.ps1 -Python 'C:\Path\To\python.exe'
```

After that one-time setup, start everything with **one command** from this folder:

```powershell
powershell -ExecutionPolicy Bypass -File .\start.ps1
```

Or **double-click `Start.cmd`**. The launcher opens your browser automatically.

The app runs at **http://127.0.0.1:8000**; API documentation is at `/docs`. Keep the single terminal open while using the app; press Ctrl+C to stop. Python serves both the backend and the built React interface. Scripts do not change machine execution policy; Bypass applies to that process only.

For frontend development, install Node.js 22 LTS+ and run `setup.ps1 -RebuildFrontend`; the original `start-backend.ps1` and `start-frontend.ps1` remain available for development with automatic refresh. If using the prebuilt package, install frontend development dependencies with `npm.cmd ci` from `frontend/` before running `verify.ps1`.

Demo works without credentials. Six clearly synthetic companies and six example partners are seeded. A demo run exercises accepted companies, a $21M rejection, duplicates, partner rotation, live progress, and partial-target reporting. Run again to see existing-company deduplication. Demo never calls external APIs or syncs Sheets. Its `.example` links are deliberately non-production.

## What you can do

- Dashboard with actual database counts, country breakdown, activity, and recent runs.
- Search/filter the company database; inspect company profiles, funding, founders, business fields, exact evidence excerpts and source links; export CSV.
- Start a sourcing run with a target and partner limit; follow progress every 1.5 seconds; request cancellation; inspect persisted run decisions.
- Add partners, include/exclude them, and rotate least-recently-sourced first. Only one sourcing or sync operation runs at a time.
- Review audit records for accepted/rejected companies, duplicates, failures, partner activity, cancellation and sheet sync.
- Select **Write accepted companies to Google Sheets** in a live run to source, qualify, write each accepted company immediately, and verify it. Manual sync remains available for pending rows. Accepted-company and verified-write counts are separate.

## Live mode

Edit `.env` locally. Do not send credentials through the frontend or commit them:

```dotenv
APP_MODE=live
DATABASE_URL=sqlite:///./nventures-live.db
OPENROUTER_API_KEY=your-new-key
TAVILY_API_KEY=your-new-key
OPENROUTER_MODELS=openrouter/free
SHEETS_SYNC_ENABLED=false
```

Restart the backend. **Use a separate database for live mode.** Mixing demo and real company databases is blocked. Relative SQLite paths resolve from `backend/`; use an absolute URL if you want another location. API keys from the old Colab conversation should be replaced rather than reused.

Add real partners through the Partners page. Live research uses Tavily search and OpenRouter JSON extraction through Python's standard-library HTTP adapter. Comma-separated `OPENROUTER_MODELS` enables ordered fallback; the default `openrouter/free` router supplies free-model routing. Actual model availability and API pricing depend on the provider. Requests have timeouts, bounded retry/backoff for rate limits and transient errors, and no retry for credential errors. Live runs may consume API credits.

The pipeline discovers explicitly related portfolio companies, skips known names before deep research, researches and enriches missing fields, discards quotes not found verbatim in retrieved search content, applies Python qualification rules, deduplicates by normalized name/domain/company LinkedIn, and saves accepted companies immediately. An individual candidate or partner failure is audited and does not stop remaining work. Default research budget: 45 candidates. Target counts are goals; the app reports partial results instead of loosening the rules.

## Google Sheets (optional)

Install only if needed:

```powershell
.\.venv\Scripts\python.exe -m pip install -r backend\requirements-sheets.txt
```

Set these backend environment variables in `.env`:

```dotenv
GOOGLE_SERVICE_ACCOUNT_JSON=<single-line JSON or base64-encoded service-account JSON>
SPREADSHEET_ID=<your spreadsheet ID>
SOURCING_TAB_NAME=Active Sourcing
PARTNER_TAB_NAME=Partner List
CONTROL_TAB_NAME=Sourcing Control
SHEETS_SYNC_ENABLED=true
```

Enable Google Sheets API and share the spreadsheet with the service-account email as Editor. No spreadsheet ID or account is hard-coded. The sourcing tab must have a `Company Name` header. Existing headers determine row order and width, so the 38-column legacy sheet remains usable. Its `Product Descriptoon`, `Unique Value Propsotions`, trailing-space founder name, and repeated `Extra Notes` headers are supported. Unmapped workflow columns stay blank; existing rows are never overwritten.

The partner tab expects `Company Name`, `Website`, and optional `nVentures Relevance` (`No` means excluded). The control tab must already exist with these headers: `Partner Name`, `Last Sourced UTC`, `Run Count`, `Last Run Added`. The app reads partner rotation and prior counts from it. When immediate sheet writing is selected it updates control state after each attempted partner.

When Google credentials and an ID are configured, live runs read existing sheet identities and partners **even if writes are disabled**. If Sheets fails, the run fails before discovery instead of silently bypassing sheet deduplication. With no Google configuration, sourcing checks only the local database; sync performs a fresh remote duplicate check later.

Accepted companies always save locally first. With **Write accepted companies to Google Sheets** selected in the live-run dialog, each newly accepted company is written and verified immediately. **Settings → Sync qualified companies** retries pending/error real companies when sync is enabled. RAW input prevents research text from becoming sheet formulas. Append responses provide the actual written range; a separate read verifies every cell. A retry first checks for an exact existing row. A matching identity with different content is marked `already_exists` and is not appended or overwritten. Errors remain visible and retryable. A timeout is never blindly followed by another append.

Settings now shows live readiness and offers **Test live connections** (read checks; small Tavily API usage, no sheet writes). See [real sourcing setup](docs/LIVE-SOURCING.md). Key presence does not imply a successful connection.

The UI follows nVentures' white/charcoal/blue website branding with the supplied official logo and Singapore image. All assets are served locally; no external font, branding service, or additional frontend dependency was introduced.

## Qualification rules

Actual supported HQ in Afghanistan, Bangladesh, Bhutan, India, Maldives, Nepal, Pakistan, Sri Lanka, or Singapore; B2B; actively operating; bootstrapped/angel/pre-seed/seed; and known total historical funding in USD ≤ $3M. All gating facts require evidence. Missing funding is not zero. The funding excerpt must explicitly support the total amount. Series A+ and conflicting over-limit funding evidence veto acceptance independently of an AI boolean. Founder nationality or an Indian market does not establish eligible HQ. There is no currency conversion dependency: unsupported non-USD totals remain unverified.

The qualification engine intentionally fails closed and can reject eligible companies when source evidence is incomplete. Research excerpts are supporting evidence, not independently authenticated facts; analysts should inspect sources before investment decisions. No override button bypasses the hard rules.

## Tests and verification

```powershell
powershell -ExecutionPolicy Bypass -File .\verify.ps1
```

Tests use `unittest` in Python and Vitest for frontend link/funding display safety. They cover all nine countries, the $3M boundary, unknown HQ, founder-nationality confusion, funding/stage contradictions, booleans, duplicate normalization, JSON parsing/model fallback, 429 retries, removal of hallucinated excerpts, legacy sheet aliases, full-row read-back, write reconciliation, demo network isolation, cancellation, partial targets, and unique constraints. See `docs/VERIFICATION.md` for the checks actually run during this build.

## Existing Streamlit project and migration

Implementation source of truth: supplied `nventures-sourcing-0.1.0.Alpha.zip`, especially `sourcing_engine.py` and `google_sheets.py`. The repository URL was supplied but its contents could not be fetched in this session. The ZIP contains user databases; neither users/password hashes nor those databases are bundled or seeded here. Original files remain unchanged. See `docs/LEGACY-MIGRATION.md` for preserved behavior and deliberate changes.

## SQLite → PostgreSQL

SQLAlchemy models use portable types and Alembic tracks schema version. PostgreSQL is optional; see `docs/POSTGRESQL.md` for migration and verified data-copy steps. No server has been provisioned or deployed.

## Current boundaries

Localhost only, one backend process/worker. No multi-user authentication or internet exposure in this build. Existing Streamlit user management and the separate worldwide Sri Lankan-founder/discover-partners workflows are outside the requested HQ mandate. Running jobs interrupted by server restart become `interrupted`; saved companies remain. Cancellation waits for the current bounded network operation. Run progress is polling rather than a separate message infrastructure. Future hosting requires authentication, TLS, multi-worker coordination and deployment-specific verification.

Real OpenRouter/Tavily requests and live Google Sheets writes have not been exercised with user credentials. Adapters were checked against official documentation and tested with fakes. Start with a small live run and a copy of the spreadsheet when configuring those integrations.

Provider references: [Tavily search](https://docs.tavily.com/documentation/api-reference/endpoint/search), [OpenRouter completions](https://openrouter.ai/docs/api/api-reference/chat/send-chat-completion-request), [Google Sheets append](https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/append).
