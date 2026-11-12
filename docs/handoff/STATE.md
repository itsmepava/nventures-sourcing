# State at handoff — 12 November 2026

## What exists

The implemented app is the recovered **Python FastAPI + React/TypeScript/Tailwind** workspace. The Go rewrite is requested and planned; no Go application has been implemented yet. This distinction must remain visible in release notes and progress reports.

Local workspace: `/home/pavara/Documents/ChatGPT/nVentures Sourcing`.

Actual app root: `outputs/nventures-sourcing-2/` under that workspace. A GitHub clone of the prerelease source has these files at its own root. Root `Start nVentures.sh` delegates to this app's `start.sh`. Do not create a third disconnected app because the workspace root looks empty.

Known-good baseline on Linux:

- 28 backend unittest checks and 4 frontend Vitest checks passed after recovery.
- TypeScript and Vite production build passed; compiled frontend exists under `frontend/dist/`.
- `sh start.sh` runs one Python process and serves UI/API at `http://127.0.0.1:8000`.
- Demo sourcing/API smoke checks, fake Sheets verification, two-message demo pitch intake, original image attachments, dashboard and dark mode were verified.
- Daily startup needs no Node.js. Current .venv was created using the bundled Codex Python runtime because system Python lacked ensurepip. It already contains the backend dependencies.

Read `docs/RECOVERY.md` for reconstruction limits. No old nVentures database or private .env was recovered from the supplied Codex archive. Current local records are demo fixtures and rehearsal history, not restored investment data. Google spreadsheets exist separately and were not altered during recovery.

## Source map

| Path | Behavior to preserve/replace |
|---|---|
| backend/app/rules.py | Hard outbound mandate, evidence and identity normalization |
| backend/app/integrations.py | Tavily/OpenRouter adapters, backoff, legacy 38-column aliases, Sheets RAW write + full-row verification |
| backend/app/engine.py | Partner rotation, job isolation, local save and optional immediate outbound sync |
| backend/app/inbox.py | Account verification, read-only Gmail, per-message parsing, broad pitch queue, six-column sync/review |
| backend/app/db.py and migrations/ | Current SQLite schema, audit, run/sync fields, migrations 0001–0003 |
| backend/app/main.py | Localhost API, readiness, connections, dashboard, companies/partners/runs/audit, static mount |
| frontend/src/ | Existing workflows and theme/preferences/keyboard/table accessibility behaviors |
| frontend/public/brand/ | Recovered official logo, icon and Singapore photograph |
| connect_google.py | Desktop OAuth PKCE/state helper; Windows/Linux wrappers exist |
| backend/tests/ and frontend/src/*.test.ts | Regression fixtures to port and extend |

## Credentials and tool availability

At preparation time, APP_MODE is demo, Sheets sync is disabled, and Tavily/OpenRouter/Google OAuth credential values are absent. Never recover stale exposed keys from chat history. Set up legitimate new credentials only if needed; the redesigned default does not require Tavily/OpenRouter.

The Codex Gmail/Drive connection is not a credential source for a standalone Go app. Google consent/client credentials are an external prerequisite for live mailbox/Sheets access; preflight/test fixtures must work without them. Keep the expected mailbox and both sheet destinations configurable privately.

Neither `go` nor `claude` was found on the current command PATH during preparation. `scripts/bootstrap_go.py` can install a verified Go toolchain locally; Claude Code's own installation/account authentication is outside this source tree. A handoff prompt is supplied in START-CLAUDE.md. Do not assume a running Claude process or a Go build already exists.

The user may have the Python app running on 8000. Inspect first and use another documented port for comparison. Do not kill the user's process as part of a test.

## GitHub and release work

Repository: https://github.com/itsmepava/nventures-sourcing (public; authenticated owner is itsmepava).

At inspection, main was `2d6eb6f6bbf3a2c83efda5896a1834b1b04cd5de` and the existing prerelease was `v0.1.0.Alpha`. Main contains the original Streamlit code, not this recovered rewrite. It also contains old database files; do not copy those into the new source/release or change unrelated history during the handoff.

The user authorized publishing the current update as a new prerelease. Prepared version: **v0.2.0-alpha.1**, a Python/React localhost baseline plus Go handoff documents. Verify actual tag/release/commit state in GitHub before continuing; a prepared release document is not proof of publication. Use a dedicated release branch and retain the old tag/history. Never describe this baseline as an implemented Go rewrite.

If you later publish the completed Go redesign, use a subsequent prerelease, with genuine build checks and release assets. Never replace the v0.2.0-alpha.1 tag in place. No hosted deployment or server purchase was requested.

## Known gaps to resolve in Go

- Existing inbound approval waits until another import to sync; implement selected **Sync now**.
- Existing extraction handles structured plain-text forms best; direct/HTML-only mail and many deck formats need review. Do not advertise automatic deck research until implemented.
- Existing normalization can still miss aliases or conflate weak identities. Add explicit provenance and reconciliation tests.
- Original full research depends on Tavily/OpenRouter; the provider replacement design deliberately exposes reduced discovery/evidence coverage instead of relaxing qualification.
- Missing live credentials prevent real-provider verification today, not building/testing a complete local application.
