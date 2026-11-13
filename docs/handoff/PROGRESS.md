# Execution progress

Updated 13 November 2026 by Claude Code on branch `claude/go-redesign`. Every item below records an executed check; mock-backed and live-provider verification are listed separately.

## Implemented baseline (before Go)

- [x] Recovered Python/React app; 28 backend + 4 frontend tests passed (re-run 12 Nov via `scripts/handoff_preflight.py`: still passing).
- [x] Source checkpoint: annotated tag `checkpoint/pre-go-redesign` → `f96c45b` (local).
- [x] Consistent SQLite backup of the Python database via the sqlite3 backup API (read-only source) in `.handoff/baseline-20261112-131452/` (git-ignored, 0600). SHA-256 of both `.env` files, `nventures.db` and `nventures.db-wal` recorded there; re-verified unchanged after all migration tests.

## Go implementation

- [x] **Phase 0** — Go 1.27.4 from `.tools/go`; modules pinned in `go.mod`/`go.sum` (`go mod verify`: all modules verified). Python-schema migration fixtures in `internal/store/testdata`.
- [x] **Phase 1** — Single binary (`cmd/nventures`): embedded UI/templates/migrations, SQLite (WAL, foreign keys, busy timeout, immediate transactions, 0600/0700 permissions), loopback default, Host/Origin/custom-header checks, CSP and security headers, optional access token for non-loopback binds, `/health`, `/readyz`, graceful shutdown, instance lock, log file without secrets.
- [x] **Phase 2** — Inbound Gmail funnel with fixtures covering every acceptance case (two companies in one thread, HTML-only, forwarded, Reply-To, quoted repeat, malformed year, thin description, same founder/different company, consumer non-South-Asia, seed >$3M, Series A, established/public, newsletter, test, prompt injection, Unicode/formula text). Approve/edit/skip; selected **Sync now** without rescanning.
- [x] **Phase 3** — First-party crawler and deterministic extraction replace Tavily/OpenRouter; optional loopback-only Ollama helper (opt-in, cloud models refused, output validated).
- [x] **Phase 4** — Strict outbound mandate ported with extra contradiction reasons; partner rotation/control state; manual URL research; candidate outcomes (accepted / rejected / needs research / duplicate).
- [x] **Phase 5** — Durable `sync_ops`, reconciliation before every append, RAW append once, full-row read-back; uncertain/conflict states; never overwrites rows or Status.
- [x] **Phase 6** — Migration, rehearsal, browser QA, release build, docs (below).

## Verification results (12 Nov 2026)

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `go test -count=1 ./...` | pass — config, discovery, domain, extract, google, httpapp, jobs, llm, netx, store |
| `go test -race` jobs, store, httpapp, discovery | pass |
| `nventures rehearse` (fixtures, no credentials) | PASS: 15 messages → 7 verified rows, 4 review, 3 skipped, 1 duplicate; approved pitch synced without rescan; sourcing 9/10 accepted (Partial), 4 rejected, 2 needs research, 1 duplicate, 9 verified rows; repeat: 0 new records, 0 new rows |
| Sync faults (fixture) | lost response, 500 before write, missing range, bad read-back, 429 on read, restart mid-write: all reconciled without duplicate rows |
| Concurrency | 8 simultaneous starts → 1 job, 7 refused |
| SSRF | private/loopback/link-local/metadata/CGNAT/IPv6 ULA, DNS answer to loopback, redirect to loopback, credentials-in-URL, non-HTTP schemes: refused |
| Clean launch | binary run with `env -i PATH=/nonexistent`: serve, health, readyz, rehearse PASS |
| Migration (real data copy) | alembic 0003; 6 companies, 6 partners, 1 run, 3 inbox jobs, 10 events, 2 pitches; counts match, 0 field mismatches, repeat imports 0, originals byte-identical |
| Browser QA (Chromium) | dark + light, desktop + 375 px mobile (no horizontal scroll); import, review drawer, approve & sync, sourcing run with live events, evidence drawer, settings, demo reset with confirm, command palette, shortcuts dialog with focus return, mobile nav + Escape |
| Bugs found in QA and fixed | sentence splitter truncated "USD 1.2 million" to $1 (regression tests added with exact amounts for all nine companies); CSP blocked inline styles; `[hidden]` overridden by CSS; `null` rendered as text; null JSON lists |
| Dependencies | direct: `modernc.org/sqlite v1.62.0`, `golang.org/x/net v0.62.0`; linked into binary: 10 modules, all MIT/BSD-3 |

## v0.3.0-alpha.2 audit and completion (12 Nov 2026)

Audit against CLAUDE.md / IMPLEMENTATION.md / ACCEPTANCE.md found dark mode **present** in alpha.1 but only reachable via an unlabelled icon, `t` or Settings, and *System* did not follow OS changes in all browsers. Real gaps found and fixed: no clear-filters action; pitch CSV ignored filters; no visible table loading state, offline state or retry on errors; no duplicate-click protection; no unsaved-edit protection or inline validation; no job retry; no links to the actual sheet row; no cross-module view of one company; themeless login page; invisible keyboard row cursor; outbound connection test failing against the fixture (`1:1` range); stale assets possible after upgrade.

| Check | Result |
|---|---|
| `go vet ./...`, `go test -count=1 ./...` | clean / pass (10 packages) |
| `go test -race` jobs, store, httpapp, discovery | pass |
| `nventures rehearse` | PASS — 16 messages → 8 verified rows, 4 review, 3 skipped, 1 duplicate; sourcing 9/10 Partial, 9 verified rows; repeat: 0 new records, 0 new rows |
| Schema upgrade 1 → 2 | unit test + copies of the user's demo DB and of the migrated-Python Go DB: all records identical after upgrade |
| Live app | user's app on 8765 restarted as 0.3.0-alpha.2 via `start-go.sh`; schema 2; data kept (pre-upgrade backup in `.handoff/pre-upgrade-demo-20261112-145902.db`) |
| Browser QA (Chromium, fixture data) | Light/Dark/System via labelled control; explicit Dark kept under light OS and after reload; System followed a live OS switch; `g c`, `g t`, `t`, `j`, `/`, Ctrl+K, Escape; unsaved-edit guard on Escape and on navigation (URL restored on cancel); inline validation focuses the bad field; double-click started one job; clear filters + filtered empty state; search persisted across reload; offline banner and auto-reconnect; Retry created job with `retry_of`; row link opened the exact verified row; cross-module panel linked GridLoom pitch ↔ company; connection test: 3/3 OK, 0 rows written; mobile 375 px no horizontal scroll |
| Screenshots | `docs/screenshots/` (overview light/dark, companies light, pitch review dark, settings dark, mobile inbox dark) |

## v0.4.0-alpha.1 — team workspace MVP (13 Nov 2026)

User request: build the full nVentures workspace unattended, with roles (admin, sourcer, pitch responder, viewer), a clean branded UI with no errors, Go backend, and **frontend and backend kept separate**. Google connection and team access from other machines were explicitly deferred by the user.

Delivered: accounts and roles (schema 3), sessions, first-run setup, role checks on every route, team management, assignments, stages and follow-ups, notes, My work, notifications, reply templates and drafts (no sending), schedules, per-person preferences, actor attribution. The backend is JSON-only; the frontend is a standalone ES-module app in `web/src`, embedded for one-command use.

| Check | Result |
|---|---|
| `go vet ./...` / `go test -count=1 ./...` | clean / pass (11 packages) |
| Role × endpoint matrix test | 4 roles × 30 endpoints: allowed never 401/403, forbidden always 403, no 5xx |
| `-race` httpapp, jobs, store, discovery | pass |
| `nventures rehearse` (production build 0.4.0-alpha.1) | PASS, now including first-run setup |
| Upgrade schema 2 → 3 | the user's demo DB copy (9 companies, 16 pitches, 52 events) and the migrated-Python DB copy: all records identical; existing pitches start at stage "new" |
| Embedded frontend | all assets 200 with content ETags, 304 on revalidation, 404 for unknown paths; setup screen loads with zero console messages |
| Browser sweep | admin 14 views, responder 11, sourcer 10, viewer 11: no error states, no JS errors; locked controls verified per role |
| UI flows | setup validation, setup, add member with one-time password, forced password change, sign-in, sign-out, assign + follow-up + note, reply draft filled, notification shown, schedule added and run |
| Fixed during QA | spurious offline banner after a cut-off request; "Completed" shown for active members; cramped schedule list; title wrapping; stacked summary cards; template order; scroll position on navigation; tab title on sign-in |

QA accounts for the throwaway QA workspace are in `.handoff/qa3-accounts.txt` (git-ignored). On the first launch after this upgrade, the user's own app shows **Set up your workspace** to create their admin account; their existing demo data is kept.

## Not verified (external)

- **Live Gmail/Sheets**: no OAuth client or consent available. Verified only against the local fixture implementing the same API contracts.
- **Windows/macOS executables**: cross-compiled, not executed.
- **Local model**: tested against a fake Ollama server; none installed.

## External setup (user-operated)

1. Google Cloud project: enable Gmail API and Sheets API; consent screen (Internal, or External testing with the mailbox as test user); scopes `gmail.readonly` and `spreadsheets`.
2. Create an OAuth **Desktop** client; download JSON.
3. `APP_MODE=live`; Settings → store client file → Connect Google as the expected mailbox.
4. Settings → confirm inbound/outbound sheet IDs and tabs; Test connection (read-only).
5. First live import with a small limit and no sync; review, then sync.

## Latest checkpoint

13 Nov 2026 (cloud session): re-verified on Go 1.27.4 in a Claude Code cloud container on branch `claude/go-redesign-b8xpo1` (from `9f4ebeb`). `go vet ./...` clean; `go test -count=1 ./...` all packages pass; race tests for httpapp/jobs/store/discovery pass; `go run ./cmd/nventures rehearse` prints PASS. One fix: `TestSSRFProtections` assumed the host resolver answers `localhost.`; the cloud sandbox resolver answers nothing, so that one DNS-loopback sub-check now runs only when the resolver can answer it. The dial-time private-address guard is still covered by the redirect-to-127.0.0.1 case, and the fetcher still ignores environment proxies. Correction to the entry below: the **v0.4.0-alpha.1 GitHub prerelease is not published**. The latest published release is v0.3.0-alpha.2. Creating the prerelease was blocked by the permission checker, so publish only on explicit request: `VERSION=0.4.0-alpha.1 sh scripts/build_release.sh`, target `9f4ebeb` or later.

13 Nov 2026: v0.4.0-alpha.1 (team workspace MVP) committed and pushed (prerelease not yet published; see above). The next session can continue in a Claude Code cloud session from branch `claude/go-redesign`. In the cloud: run `python3 scripts/bootstrap_go.py` (or use an installed Go 1.26+), `go test ./...`, `go run ./cmd/nventures rehearse`. There is no `.env`, no Python app and no user data there, which is fine because demo mode needs none.

12 Nov 2026 (later): v0.3.0-alpha.2 committed locally on `claude/go-redesign` (not yet pushed or released).


12 Nov 2026: branch pushed; prerelease **v0.3.0-alpha.1** published at https://github.com/itsmepava/nventures-sourcing/releases/tag/v0.3.0-alpha.1 (tag → `8847458`; 4 platform binaries, source archive, DEPENDENCIES.txt, SHA256SUMS; downloaded assets re-verified against SHA256SUMS). v0.2.0-alpha.1 is unchanged.

Next action: perform the live Google connection (External setup above), run one small live import and one outbound sync to a test tab, and record the results here.
