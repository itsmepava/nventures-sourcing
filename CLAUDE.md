# Claude Code: implement nVentures Go

## Authorized objective

The user requests a complete redesign of this internal VC app in **Go**, an all-in-one localhost application, minimal third-party dependencies and operating cost, with replacements for Tavily and OpenRouter. Deliver a working product suitable for a presentation and daily sourcing/pitch intake. Implement and verify it end to end; do not stop at planning or UI mockups. Make ordinary implementation decisions without asking the user. Observe actual permissions and authentication boundaries.

Read these before implementation:

1. `docs/handoff/IMPLEMENTATION.md` — ordered work and fixed architecture decisions.
2. `docs/handoff/PROVIDERS.md` — replacement strategy and its practical limits.
3. `docs/handoff/ACCEPTANCE.md` — completion criteria and verification matrix.
4. `docs/handoff/STATE.md` — existing code, credentials and release status.
5. `docs/handoff/GOOGLE-CLOUD.md` — localhost development and later Google Cloud readiness.

## Working rules

- Preserve the current Python/React app as a regression reference until Go passes functional parity. Keep its existing launcher working. Back up SQLite consistently before any migration. Never delete `.env`, databases, user records or history to make tests pass.
- Keep inbound and outbound qualification separate. Inbound is a broad startup funnel with review for unclear/mature businesses. Outbound has the original hard mandate documented in the implementation plan. No invented funding amounts, HQ or stage evidence.
- Default to Go `net/http`, `html/template`, `embed`, `database/sql`, standard MIME/JSON/CSV/crypto libraries and small local JS/CSS. No web framework, ORM, Node/Python runtime, container stack or hosted LLM is required for normal Go use. Default approved direct Go dependencies are a SQLite driver and an HTML parser; explain and measure transitive dependencies.
- No paid search or model requests by default. First-party crawling plus deterministic evidence extraction is the default. Optional local-model assistance must work offline after a model is already installed, remain opt-in and fail safely. Do not quietly call a cloud model through a local gateway.
- Only read Gmail. Do not send/reply, archive, mark read, delete or relabel mail. Keep Google Sheets writes explicit and verified. Never alter the investment Status field or overwrite manual decisions.
- Never retrieve credentials from the Codex chat archive or connector sessions. Use existing local environment variables or legitimate Google authorization. No secrets in the frontend, logs, Git or release ZIPs.
- Localhost only; do not deploy or expose the app remotely. Preserve Windows support where practical; Linux is the user's active system.
- Run relevant tests and browser QA, record real outcomes, fix failures and continue. If Google credentials are absent, finish the entire deterministic/mock-backed application and present a specific connection checklist; do not claim a live run succeeded.
- Do not stop between phases for routine confirmation. Keep `docs/handoff/PROGRESS.md` current so work can resume across sessions. Use checkpoints and local backups before risky changes. Never force-push or delete existing repository history.

## Start immediately

Run `python3 scripts/handoff_preflight.py`. If Go is absent, run `python3 scripts/bootstrap_go.py`; it installs a checksum-verified official Go SDK into `.tools/` without changing system packages. Add `.tools/go/bin` to the task's PATH. Implement phases in order and finish with `go test ./...`, `go vet ./...`, a production binary, the deterministic presentation script, migration verification and browser checks. The current baseline has 27 Python and 4 frontend tests.

User-facing completion requires a launchable Go binary and tested complete workflows, not merely a Go scaffold or disconnected frontend. Tell the user what works, what was tested and exactly which external credentials remain, if any.
