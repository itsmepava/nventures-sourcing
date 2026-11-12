# nVentures Sourcing v0.3.0-alpha.2 — daily-use and workflow completion

Builds on v0.3.0-alpha.1. Same single executable, same two direct dependencies. Existing databases upgrade automatically (schema 1 → 2; one added column per table, no data rewritten).

## Changed

- **Theme control is now labelled** (System / Light / Dark) in the top bar. Dark mode existed in alpha.1 but was only reachable through an unlabelled icon, the `t` shortcut or Settings. *System* now also follows OS changes while the app is open, and the browser chrome colour follows the theme. The sign-in page and demo sheet viewer are themed too.
- **Duplicate-submission protection:** every button and form ignores repeat clicks while its request is running and shows a busy state.
- **Unsaved-edit protection:** closing a pitch or partner panel, pressing Escape, navigating away or reloading after editing asks before discarding.
- **Inline validation** with messages next to the field (company name, description and contact email to approve; website format; partner name/website).
- **Clear filters** in every table toolbar and in filtered empty states; visible loading state; error states with **Retry**.
- **Offline banner** when the local app stops, with automatic reconnection and a *Retry now* button.
- **Pitch CSV export now honours the current search and filters** (company export already did).
- **Links to the actual sheet row** after verified sync (Google Sheets `#gid=…&range=A<row>` in live mode; a local fixture sheet viewer in demo mode).
- **Across nVentures** panel on pitch and company profiles: the same company's sourced record, pitches, research outcomes and activity, matched only by normalised name or non-shared website domain.
- **Retry** for failed, cancelled, interrupted, partial or error-containing jobs, with the original settings (`retry_of` recorded).
- **Bug fixes:** the outbound connection test asked for row range `1:1`, which the fixture rejected (now `A1:AZ1`); the keyboard-selected table row had no visible highlight; static assets are now versioned so upgrades never serve stale CSS/JS.
- Demo mailbox gains a sixteenth fictional message (a portfolio company that also pitches) to demonstrate cross-module identity.

## Verified (12 Nov 2026)

`go vet` clean; `go test ./...` pass (10 packages) incl. new upgrade, related-records, retry, row-link and connection-test tests; `-race` clean (jobs, store, httpapp, discovery); `nventures rehearse` PASS (16 messages → 8 verified rows, 4 review, 3 skipped, 1 duplicate; sourcing 9/10 Partial, 9 verified rows; repeat adds nothing). Upgrade of real schema-1 databases (your demo database and the migrated Python data) preserved every record. Browser QA: light/dark/system incl. live OS switch and reload persistence, keyboard (`g`+letter, `t`, `j/k`, `/`, Ctrl+K, Escape), dialogs with focus trap/return, double-click protection, validation, unsaved-edit guard on Escape and navigation, clear filters, offline/reconnect, retry, row links, mobile 375 px. Screenshots in `docs/screenshots/`.

## Still not verified

Live Gmail/Sheets (no Google credentials yet), Windows/macOS executables (not run), local model (none installed). See `docs/GO-APP.md` → *Connect Google*.
