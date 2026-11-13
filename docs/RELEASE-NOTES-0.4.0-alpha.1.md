# nVentures Workspace v0.4.0-alpha.1 — team workspace MVP (prerelease)

Builds on v0.3.0-alpha.2. Still one Go executable with two direct dependencies (`modernc.org/sqlite`, `golang.org/x/net`); password hashing uses Go's standard library. Existing databases upgrade automatically (schema 2 → 3); nothing is rewritten.

## New

- **Accounts and roles.** First-run setup creates the admin; every person signs in with their own account. Roles: **Admin**, **Sourcer**, **Pitch responder**, **Viewer**. The server checks the role on every API route (tested for every role against every endpoint); the UI locks unavailable controls and says why. One-time passwords for new members, forced password change, admin resets, deactivation, last-admin protection, 15-minute lockout after 5 failed sign-ins, hashed session tokens, 7-day sliding sessions.
- **Team workflow.** Assign pitches and companies to people, move them through stages, set follow-up dates, and keep internal notes. **My work** shows what is assigned to you, follow-ups first. All of it stays in nVentures; nothing is written to the sheets.
- **Notifications** for assignments, pitches needing review, and scheduled job results.
- **Reply drafts** for pitch responders from admin-managed templates, opened in the responder's own email program. nVentures never sends email, and Gmail access stays read-only.
- **Automation.** Admins schedule inbox imports and sourcing runs (hourly to weekly), optionally with verified sheet writes. Scheduled runs use the same rules as manual ones and are labelled "Scheduler" in history.
- **Attribution.** Every audit event and job records who did it; preferences are per person.
- **Separate frontend.** The backend is now a JSON-only API with no server-rendered pages. The UI lives in `web/src` as plain HTML/CSS and native ES modules (see `web/README.md`). It is embedded in the binary for one-command use; `NV_WEB_DIR=web/src` serves it from disk during development. Embedded assets are served with content ETags.

## Changed

- `NV_ACCESS_TOKEN` is replaced by accounts. A non-loopback bind now needs `NV_ALLOW_REMOTE=1` and `NV_BASE_URL`. The default remains this computer only.
- A request that fails once (for example during navigation) no longer shows the offline banner unless a health check confirms the app is unreachable.
- UI polish: top-bar labels collapse on medium screens, the page scrolls to the top on navigation, summary cards wrap responsively, and reply templates list in creation order.

## Verified (13 Nov 2026, demo fixtures)

- `go vet` clean.
- `go test ./...` passes in all 11 packages, including setup/sign-in/sign-out, lockout, team guards, the full role × endpoint matrix, assignment notifications, queue, notes and audit attribution, and scheduler runs with notifications.
- `-race` clean for httpapp, jobs, store and discovery.
- `nventures rehearse` PASS.
- Upgrading copies of real schema-2 databases (the demo data, and Python data migrated earlier) preserved every record.
- Browser QA (Chromium): setup, sign-in, forced password change and sign-out through the UI; a sweep of every page and drawer as admin, sourcer, pitch responder and viewer found no error states and no JavaScript errors. Normal sign-in and sign-out produced no failed requests; the only 401s came from test scripts signing out behind an open page, which then correctly returned to sign-in.
- Light and dark screenshots are in `docs/screenshots/`.

## Not verified / limits

- Live Gmail and Sheets are still unverified: no Google credentials are configured.
- Windows and macOS builds were cross-compiled but not run.
- Schedules run only while the app is running on this computer.
- The workspace is reachable only from this computer. Giving the team access from other machines is a separate hosting decision (`docs/deploy/GOOGLE-CLOUD.md`).
