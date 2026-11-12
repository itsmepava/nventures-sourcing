# Execution progress

Updated 12 November 2026 by Claude Code on branch `claude/go-redesign`. Every item below records an executed check; mock-backed and live-provider verification are listed separately.

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

Next action for a future session: perform the live Google connection above, run one small live import and one outbound sync to a test tab, record results here, then publish v0.3.0-alpha.1 as a GitHub prerelease with the artifacts from `scripts/build_release.sh` (requires the user's go-ahead and GitHub authentication).
