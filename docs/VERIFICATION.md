# Verification performed on 14 October 2026

Windows localhost; bundled Python 3.12, Node 22+; no deployment.

| Check | Result |
| --- | --- |
| Backend `unittest` regression suite | 20 tests passed after branding/immediate-write update |
| Frontend Vitest suite | 2 tests passed |
| TypeScript compile and Vite production build | Passed; JS approximately 91 KB gzip, CSS approximately 7 KB gzip |
| All delivered Python source syntax | Passed |
| Fresh Alembic initial schema and SQLite database transfer | Passed in isolated temporary databases |
| Legacy run-count import, source unchanged, idempotency | Passed in isolated temporary databases |
| Localhost health/API/frontend HTTP | Successful |
| API smoke: reads, search, profile, CSV, 404/422, blocked demo sync, blocked foreign Origin | Passed |
| API smoke: start, concurrent start 409, cancel, persisted audit | Passed |
| Browser: overview, start demo run, live progress, completed partial report | Verified |
| Browser: company search and detailed profile with evidence | Verified |
| Browser: console errors/warnings after final reload | None observed |
| Dashboard screenshot | `dashboard.jpg`, synthetic demo data |
| Live Tavily/OpenRouter and actual Sheets write | Not performed; no user credentials used |
| Actual PostgreSQL server | Not exercised; SQLite-to-SQLite transfer tested, PostgreSQL pathway supplied |

Browser demo run #1 started with six seeded companies, accepted six new companies, rejected one $21M company, skipped seven duplicates, and recorded zero errors. The 10-company target correctly returned `partial`. Later smoke checks produced a cancelled demo run. Neither run made external API calls or wrote to Google Sheets.

Reproduce the unit/build checks using `verify.ps1`. With both servers running in demo mode, run `..\.venv\Scripts\python.exe smoke_localhost.py` from `backend/` to check the running API. The smoke script intentionally creates a demo run/cancellation audit record.

This local build uses one backend process. No production hosting, authentication, external writes, or multi-worker behavior is claimed as verified.

Single-command launcher follow-up: `launch.py` successfully started one Python process on port 8000, opened the browser automatically, and served the compiled interface/assets and all dashboard API requests. The 17 backend regression tests were rerun. `setup.ps1` now builds the interface once; `Start.cmd` or `start.ps1` launches the whole app thereafter.

Branding/sourcing follow-up: inspected nventures.sg in the browser; applied the supplied logo/Singapore image and blue/white/charcoal palette; compiled frontend and passed its 2 tests. The backend's 20 tests include immediate live-run sheet writes with fake providers, retention/reporting after a sheet error, and connection reads without sheet writes. Browser checked the new branding, live-setup guidance, and disabled sheet-writing control in demo mode. No new dependency was added. The updated downloadable package includes the compiled frontend, so ordinary setup can skip Node installation/build.
