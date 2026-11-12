# Definition of done for the Go replacement

Every item needs an actual verification result in PROGRESS.md. A page screenshot, a passing build or an API returning 200 alone does not establish workflow completion.

## Packaging and dependencies

- [ ] One Go binary serves embedded UI/API/migrations, opens the local browser and shuts down cleanly. Default bind is loopback and no public server is deployed.
- [ ] A clean launch from a directory without Python/Node/Docker/Ollama on PATH works after build. Test with a deliberately restricted PATH and known fixture data.
- [ ] `go test ./...` and `go vet ./...` pass. Run race checks for the job/sync/store packages when supported. Pin toolchain/module versions; attach module/license inventory and build checksums.
- [ ] Default direct dependencies stay within the SQLite + HTML-parser budget, or each addition has a concrete justification and transitive inventory. No paid API traffic occurs in default tests/demo.
- [ ] Linux release binary runs; Windows artifact is built/tested where tooling permits, with honest limits otherwise. Source and assets are also available. No secrets, private pitch data or databases in Git/releases.

## Baseline and migration

- [ ] Original Python/React app remains launchable until Go parity is proven. Preserve .env and original DB files byte-for-byte across tests of the new migration/import.
- [ ] Migrate a fixture copy with every Python table and representative field: 38-column company data, partner rotation, run counters, audit events, pitch edits, sources, sync error, actual sheet row/range and interrupted jobs.
- [ ] Counts and important field values match before/after; demo/live records remain separate; repeated migration is idempotent and rollback instructions work. SQLite WAL backups are consistent, not a naive live .db copy.

## Gmail workflow

- [ ] Individual message/account IDs are processed incrementally. Two different companies in one Gmail thread both appear. Repeat imports and pagination do not lose or duplicate records; per-run limits report remaining work.
- [ ] Structured forms, HTML-only MIME, direct/forwarded pitches, different Reply-To, quoted repeats, a malformed year in incorporation, thin descriptions and same-founder/different-company cases are covered by fixtures.
- [ ] Consumer startups outside South Asia, seed asks above $3M, and legitimate Series A startups are allowed into the broad inbound funnel. Obvious established/public businesses and non-pitches are flagged or skipped with reasons; unclear cases require review.
- [ ] Missing/unknown HQ remains unknown; incorporation is retained separately. Startup revenue/funding isn't inferred from a founder's prior exit or a customer's size.
- [ ] Edit/approve/skip persists; selected approved records can sync immediately without rescanning Gmail. Original email/deck links and unreviewed attachment indicators work.
- [ ] No send/modify Gmail scopes or code paths. No hosted model calls for private emails. Missing OAuth credentials produce useful setup/readiness UI while demo keeps working.

## Outbound workflow

- [ ] Each of the nine allowed HQ countries plus unknown/outside HQ is tested; founder nationality/target market/office alone fails the HQ rule.
- [ ] Active B2B and bootstrapped/angel/pre-seed/seed requirements remain strict. $3M total USD funding boundary, undisclosed totals, latest-round-only amounts and contradictory Series A/maturity evidence are tested.
- [ ] Direct partner/company crawling discovers fixture companies, retains exact source quotes and flags blocked/JS-only/truncated sources. Cache/rate limits/robots/cancellation are exercised.
- [ ] Targets never relax qualification. Missing evidence, no eligible companies and partial targets are complete honest outcomes, not fabricated success.
- [ ] Partner rotation/counts/control state survive restart. A company/partner/provider failure doesn't discard successful earlier work.

## Synchronization and reliability

- [ ] Separate inbound six-column and legacy outbound 38-column mappings; typo headers, duplicate Extra Notes and unknown headers are tested.
- [ ] RAW append, entire-row read-back and actual returned row number verified against a deterministic fake Sheets server. Accepted and synced counts differ correctly on a write failure.
- [ ] Uncertain write timeout, successful append with lost response, restart during sync, existing identity with different contents, Unicode/newlines/formula-looking text and repeated retry all have tests. Never append blindly after a timeout.
- [ ] Inbound investment Status and existing manual cells/decisions are unchanged. Shared social/document-host URLs and a shared founder email cannot merge unrelated businesses.
- [ ] Jobs have persisted progress/events, cancellation, interrupted/partial outcomes, retry and one effective sync writer. Concurrent starts can't race past the guard.
- [ ] HTTP mocks verify bounded backoff/Retry-After for 429/5xx, no infinite retries on auth failures, and cancellation during sleep/network work.
- [ ] SSRF tests include private DNS answers and redirect changes; size/depth/MIME limits, hostile HTML/source instructions and CSV formula escape tests pass.

## UI and presentation

- [ ] Both workflows are complete through the UI: import/discover → evidence/review → save → explicit verified sheet sync → row/source link → history.
- [ ] Light/dark/system theme, compact mode, saved preferences, search, filters, sort, pagination, shortlist, safe CSV, progress, disabled readiness actions and helpful errors work.
- [ ] Keyboard quick actions, Escape, focus trap/return, visible focus, labels, mobile layout, sufficient contrast and reduced motion checked in browser.
- [ ] Deterministic local rehearsal completes both workflows with no external credentials; repeating it produces zero duplicate companies/rows. All sample data is labelled demo.
- [ ] If real Google/Tavily/OpenRouter credentials are absent, list that limit exactly; record mock-backed verification separately from live-provider verification. Do not call the whole product blocked while independent engineering remains.
- [ ] Provide a 5-minute presenter script, runnable binary, source backup, release notes, migration/rollback guide and remaining external setup steps.
