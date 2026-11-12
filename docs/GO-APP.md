# nVentures Sourcing — Go app (v0.3.0-alpha.1)

One executable serves the redesigned interface, the API, database migrations and background jobs on `http://127.0.0.1:8765`. Data lives in a local SQLite file. Python, Node.js, Docker, Redis, Ollama and paid provider accounts are **not** needed.

## Start

| System | How |
|---|---|
| Linux (from this repository) | `./start-go.sh` — builds `bin/nventures` on first run with the local `.tools/go`, then starts it and opens the browser |
| Linux (release binary) | `./nventures-0.3.0-alpha.1-linux-amd64` |
| Windows | Put `nventures.exe` next to `Start nVentures (Go).cmd` and double-click it |
| Any | `nventures serve --no-browser` to start without opening a browser |

Stop with **Ctrl+C**. Running jobs are cancelled cleanly; anything already saved is kept and the job is shown as *interrupted* or *cancelled*.

Other commands:

```
nventures rehearse                 # deterministic demo of both workflows, twice; prints PASS/FAIL
nventures migrate-python --from <python nventures.db> [--mode demo|live]
nventures backup [--out file.db]   # consistent SQLite backup (VACUUM INTO)
nventures version
```

The legacy Python app is unchanged and still starts with `sh start.sh` on port 8000. The two never share a database.

## Modes and data

- **Demo** (`APP_MODE=demo`, default): Gmail, Google Sheets, Google sign-in and partner/company websites are replaced by a fixture server on a random loopback port. Every record is fictional and labelled “(demo)”. Nothing leaves the computer.
- **Live** (`APP_MODE=live`): real Gmail (read-only), real Sheets, real websites.

Data folder (`NV_DATA_DIR` overrides): Linux `~/.local/share/nventures/{demo,live}/`, Windows `%LOCALAPPDATA%\nVentures\{demo,live}\`. It holds the database, the log, `backups/`, and in live mode the Google client file and token (all user-only permissions). Demo and live databases are separate files and the app refuses to open a database containing the other mode's records.

## Configuration

Environment variables win over `.env`. The `.env` reader supports `KEY=value`, `export KEY=value`, `"double quoted"` (with `\n`), `'single quoted'` and `#` comments; no variable expansion. Existing Python `.env` names are accepted unchanged.

| Variable | Meaning |
|---|---|
| `APP_MODE` | `demo` or `live` |
| `NV_PORT` / `NV_HOST` | Default `8765` / `127.0.0.1`. If 8765 is busy and `NV_PORT` is unset, the next free port up to 8775 is used |
| `NV_DATA_DIR` | Data folder |
| `GMAIL_EXPECTED_ACCOUNT` | Mailbox that must be connected; imports refuse other accounts |
| `INBOX_SPREADSHEET_ID`, `INBOX_SHEET_TAB` | Inbound sheet (six columns: Company Name, Description, HQ, Contact, Status, Notes) |
| `SPREADSHEET_ID`, `SOURCING_TAB_NAME`, `PARTNER_TAB_NAME`, `CONTROL_TAB_NAME` | Legacy outbound sheet and its partner/control tabs |
| `SHEETS_SYNC_ENABLED` | `true` allows outbound sheet writes (also toggleable in Settings) |
| `GOOGLE_OAUTH_CLIENT_ID/SECRET/REFRESH_TOKEN` | Optional: reuse a connection made with the old `connect_google.py` |
| `GOOGLE_SERVICE_ACCOUNT_JSON` | Optional legacy service account (Sheets only) |
| `GMAIL_PITCH_QUERY` | Gmail search used for imports |
| `MAX_RESEARCH_PER_RUN` | Company research cap per sourcing run (default 45) |
| `NV_OLLAMA_URL` | Optional local model endpoint (loopback only) |
| `NV_BASE_URL`, `NV_ACCESS_TOKEN` | Only for future hosting; see `docs/deploy/GOOGLE-CLOUD.md` |

Values saved in **Settings** (sheet IDs, tabs, expected account, query) are stored in the database and override `.env`.

`TAVILY_API_KEY` and `OPENROUTER_API_KEY` are detected only to report that they are present; the Go app never calls either service.

## The two workflows

**Inbound pitch funnel (Pitch inbox).** Import reads individual Gmail messages (identity = account + message ID; threads never merge companies). Plain-text and HTML-only mail, website forms, forwarded and direct pitches, Reply-To, quoted replies, attachments and deck links are handled deterministically. The funnel is broad: no outbound geography, B2B or funding limits. Established/public businesses, investors, unclear or thin submissions and messages containing instructions aimed at automation go to *Needs review*; newsletters, receipts and test submissions are *Skipped*; re-submissions of the same company are *Duplicates*. Edit, approve or skip in the review panel; **Approve & sync now** writes immediately without rescanning Gmail. Mail is never sent, archived, labelled, marked read or deleted.

**Outbound sourcing.** Partners are visited least-recently-sourced first. For each partner the crawler reads robots.txt and sitemaps, follows portfolio pages, pagination and RSS/Atom items, and collects official company links. Each company's own pages are fetched (bounded), and evidence is extracted as exact sentences. A company is accepted only if its pages evidence: HQ in Afghanistan, Bangladesh, Bhutan, India, Maldives, Nepal, Pakistan, Sri Lanka or Singapore; an active B2B business; bootstrapped/angel/pre-seed/seed stage; and **total** USD funding ≤ $3,000,000. Contradicting evidence → *Rejected*; missing evidence → *Needs research* (never assumed). Targets are goals; a run that cannot reach one ends *Partial*. Paste company URLs under **Research specific companies** for JavaScript-only or blocked portfolios.

**Sheet sync (both).** Before any write the app persists a sync record, reads the whole tab, and reconciles: an identical row is adopted as verified, a row for the same company is never overwritten (*Already in sheet*). New rows are appended once with RAW input, the returned row is read back in full and compared. A timeout, dropped connection or missing range is marked *Write uncertain* and the next sync reads the sheet before doing anything — appends are never blindly retried. New inbound rows leave **Status** blank; existing cells are never modified.

## Daily-use features

Light/dark/system theme without flash, compact density, saved per-table filters, search, sort, pagination, shortlist (star), CSV export with formula neutralisation, live job progress with cancel, job history and audit log, readiness panel with disabled actions that explain what is missing, command palette (**Ctrl/⌘ K**), shortcuts (**?**, `/`, `g` + letter, `j/k`, `x`, `s`, `t`, `c`, **Esc**), focus-trapped dialogs that return focus, mobile layout, reduced-motion support.

## Connect Google (live mode) — the one step only you can do

1. In Google Cloud Console, in a project nVentures controls: enable **Gmail API** and **Google Sheets API**.
2. **OAuth consent screen**: *Internal* if the mailbox is a Google Workspace account in the same organisation; otherwise *External* in testing with the mailbox added as a test user. Add scopes `https://www.googleapis.com/auth/gmail.readonly` and `https://www.googleapis.com/auth/spreadsheets`.
3. **Credentials → Create OAuth client ID → Desktop app.** Download the JSON.
4. Set `APP_MODE=live` in `.env` and start the app. Open **Settings → Google connection**, choose the JSON file, click **Store client file**, then **Connect Google**, sign in as the expected mailbox and approve.
5. In **Settings → Destinations** confirm the expected account, inbound sheet ID/tab, outbound sheet ID/tabs, and enable outbound writing if wanted. Click **Test connection (read-only)** — it checks the mailbox and headers and writes nothing.
6. First live import: limit 10, *without* sync. Review, approve, then **Sync selected now**.

If the account that signs in differs from the expected mailbox, nothing is saved. **Disconnect** removes the local token; revoke it at Google account permissions to make Google forget it too.

## Optional local model

Off by default. If an Ollama server is already running on `127.0.0.1` with an installed local model, Settings lets you enable **Suggest with local model** in the pitch review panel. Only loopback endpoints are accepted, cloud-backed models are hidden and refused, nothing is downloaded, and suggestions are validated against the email text and never applied automatically. Qualification, statuses and sheets are never influenced by a model.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| “another nVentures process is already using this data directory” | The app is already running; open its URL, or stop it first |
| Port in use | Set `NV_PORT=8780` (or another free port) |
| Import button disabled | Live mode without a Google connection — see *Connect Google* |
| *Write uncertain* | Network failed mid-write. Click sync again: the sheet is read and reconciled first |
| *Conflict* | Read-back differed or a matching row has different contents after an uncertain write. Inspect the sheet, then sync the item again |
| Partner shows “JavaScript-only” | Use *Research specific companies* with official company URLs |
| Logs | `nventures.log` in the data folder (no secrets, bodies or email contents are logged) |
