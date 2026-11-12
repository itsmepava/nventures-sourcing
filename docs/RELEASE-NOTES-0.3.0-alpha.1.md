# nVentures Sourcing v0.3.0-alpha.1 — Go rewrite (prerelease)

The first implemented Go version: one executable with the redesigned interface embedded, SQLite storage, the inbound Gmail pitch funnel, outbound sourcing against the mandate, and verified Google Sheets synchronisation. The previous prerelease, **v0.2.0-alpha.1**, is the Python/React baseline and is unchanged; this is a separate tag.

## What is implemented and verified

- **Single binary**, Go standard library server/templates/embedding; two direct dependencies (`modernc.org/sqlite`, `golang.org/x/net/html`). Linux binary tested; Windows/macOS/Linux-arm64 cross-compiled. Starts with an empty `PATH` (no Python/Node/Docker/Ollama).
- **No paid providers.** Tavily replaced by a first-party crawler (robots.txt, sitemaps, RSS/Atom, pagination, per-host rate limits, Retry-After, ETag/Last-Modified cache, size/MIME/redirect limits, SSRF protection incl. DNS answers and redirects). OpenRouter replaced by deterministic extraction with exact source quotes. Optional loopback-only local model for review suggestions, off by default.
- **Inbound funnel**: per-message Gmail identity, pagination with resumable limits, HTML-only/forwarded/direct pitches, Reply-To, quoted replies, malformed fields, attachment and deck indicators (marked unreviewed), broad classification with review reasons, duplicates by company name/website only, approve/edit/skip and immediate **Sync now**. Read-only Gmail scope; no send/modify code paths (test-enforced).
- **Outbound sourcing**: strict mandate (nine HQ countries; B2B; active; bootstrapped–seed; evidenced total ≤ $3M USD; independent contradiction checks), honest *Partial* outcomes, *Needs research* for missing evidence, fair partner rotation with persisted counts and control-tab mirroring, manual URL research.
- **Sheets**: separate six-column inbound and legacy 38-column outbound mappings (typo aliases, duplicate *Extra Notes*), RAW appends, full-row read-back, durable sync records, reconciliation after timeouts/lost responses/restarts, never overwriting existing rows or the Status column.
- **Reliability**: durable jobs with progress, events, cancellation, interrupted/partial outcomes, one writer per workflow via SQLite leases, single-instance lock, graceful shutdown.
- **UI**: dashboard, pitch inbox, sourcing, companies, partners, activity, settings; light/dark/system theme, compact mode, saved filters, sort, pagination, shortlist, safe CSV, command palette and shortcuts, accessible dialogs, mobile layout.
- **Migration** from the Python database (read-only, idempotent, verified field by field), plus `backup` and a deterministic `rehearse` command.

## Verification summary

`go vet ./...` clean; `go test ./...` passes (10 packages with tests); `-race` clean for jobs, store, httpapp and discovery. `nventures rehearse` PASS: 15 demo emails → 7 verified sheet rows, 4 review, 3 skipped, 1 duplicate; sourcing 9/10 accepted (Partial), 4 rejected, 2 needs research, 9 verified rows; repeating both added zero companies and zero rows. Browser QA in Chromium (desktop and 375 px mobile, light and dark). Python baseline: 28 backend and 4 frontend tests still pass.

## Not verified / known limits

- **No live Google run.** OAuth client credentials and mailbox consent were not available, so Gmail and Sheets were verified only against the deterministic local fixture that implements their API contracts. See *Connect Google* in `docs/GO-APP.md`.
- **Windows and macOS binaries were cross-compiled but not executed** (no Windows/macOS machine or emulator was available).
- A direct crawler cannot match a global search index: JavaScript-only, login-protected or unlisted portfolios are reported and need manual URLs. Evidence extraction is pattern-based; phrasing outside its vocabulary results in *Needs research*, never acceptance.
- PDF/PPTX decks are not parsed; they are listed as unreviewed with links to the original email.
- The optional local-model helper was tested against a fake Ollama server; no local model is installed on the build machine.
- Not deployed anywhere. Hosting preparation is documented in `docs/deploy/GOOGLE-CLOUD.md`.

## Upgrade

Keep the Python app. Start the Go app with `./start-go.sh` (port 8765). Import existing records with `nventures migrate-python` (`docs/MIGRATION-GO.md`). Assets: platform binaries, source archive, `DEPENDENCIES.txt`, `SHA256SUMS`.
