# nVentures Workspace — Go app (v0.5.0-alpha.1)

One executable serves the redesigned interface, the API, database migrations and background jobs on `http://127.0.0.1:8765`. Data lives in a local SQLite file. Python, Node.js, Docker, Redis, Ollama and paid provider accounts are **not** needed.

## Start

| System | How |
|---|---|
| Linux (from this repository) | `./start-go.sh` — builds `bin/nventures` on first run with the local `.tools/go`, then starts it and opens the browser |
| Linux (release binary) | `./nventures-0.5.0-alpha.1-linux-amd64` (from the release page) |
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

## Accounts and roles

The first time the app opens it asks you to **set up the workspace**: create the first admin account (name, email, password of at least 10 characters). After that everyone signs in with their own account.

| Role | Can |
|---|---|
| **Admin** | Everything: team members, settings, destinations, Google connection, reply templates, automation schedules, demo reset |
| **Sourcer** | Run outbound sourcing and company research, sync companies to the outbound sheet, edit partners, assign/stage/annotate companies |
| **Pitch responder** | Import pitches from Gmail, review/approve/skip, sync pitches, assign/stage/annotate pitches, draft replies to founders |
| **Viewer** | Read everything; change nothing |

Every role can shortlist (except viewers), use My work, notifications, filters and exports. Controls a role can't use stay visible but locked, with an explanation. The server enforces the same rules on every API call (tested for every role against every endpoint).

- **Team** (admins): add a member with a role; the app shows a **one-time password** to hand over privately. The member must choose their own password at first sign-in. Admins can change roles, reset passwords (signs the person out everywhere) and deactivate members (history is kept). The last active admin can't be removed.
- **Sessions** last 7 days of inactivity, use HttpOnly SameSite cookies, and are stored only as hashes. Five wrong passwords for the same email lock sign-in for 15 minutes. Passwords are hashed with PBKDF2-SHA256 (600,000 iterations, Go standard library).
- **Forgotten password:** an admin resets it. If the only admin is locked out, stop the app and restore from a backup or ask for help; there is deliberately no back door.
- **Who did what:** every audit event and job records the person (or "Scheduler"). Preferences such as theme and saved filters are per person.

## Team workflow

- **Assignments, stages and follow-ups** on every pitch (New → Reviewing → Contacted → Meeting → Passed/Progressed) and company (Not contacted → Contacted → Meeting → Passed/Progressed). These live only in nVentures and are never written to the sheets.
- **My work** lists everything assigned to you, follow-ups due first. Assigning someone sends them a notification (bell in the top bar, or press `n`).
- **Notes** on pitches and companies are internal, timestamped and attributed.
- **Reply to founder** (pitch responders): pick a template, edit it, then *Open in my email* opens a ready draft in your own mail program, or *Copy draft*. nVentures never sends email; Gmail access stays read-only. Admins edit templates in Settings; placeholders are `{{company}}`, `{{founder}}`, `{{me}}`.
- **Automation** (admins, Settings): schedule inbox imports and sourcing runs every hour/4 hours/12 hours/day/week, optionally with verified sheet writes. Schedules run while the app is open on this computer, use exactly the same rules as manual runs, and notify admins plus the relevant role. Inbox imports are skipped (and say so) while Google isn't connected.

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
| `NV_BASE_URL`, `NV_ALLOW_REMOTE` | Only for future hosting; a non-loopback bind needs both. See `docs/deploy/GOOGLE-CLOUD.md` |
| `NV_WEB_DIR` | Development only: serve the frontend from this folder (e.g. `web/src`) instead of the embedded copy |

Values saved in **Settings** (sheet IDs, tabs, expected account, query) are stored in the database and override `.env`.

`OPENROUTER_API_KEY` is only detected to report that it is present; the Go app never calls OpenRouter. `TAVILY_API_KEY` is used for one thing only, and only if you switch it on in **Settings → Providers & data** (live mode): searching the web for partners whose website is missing (see *Partners without a website*). Only the partner's name is sent.

## The two workflows

**Inbound pitch funnel (Pitch inbox).** Import reads individual Gmail messages (identity = account + message ID; threads never merge companies). Plain-text and HTML-only mail, website forms, forwarded and direct pitches, Reply-To, quoted replies, attachments and deck links are handled deterministically. The funnel is broad: no outbound geography, B2B or funding limits. Established/public businesses, investors, unclear or thin submissions and messages containing instructions aimed at automation go to *Needs review*; newsletters, receipts and test submissions are *Skipped*; re-submissions of the same company are *Duplicates*. Edit, approve or skip in the review panel; **Approve & sync now** writes immediately without rescanning Gmail. Mail is never sent, archived, labelled, marked read or deleted.

**Pitch decks.** A PDF or PowerPoint (.pptx) deck attached to a pitch email can be read from the review panel with **Read deck text** (pitch responders and admins). The attachment is downloaded read-only from Gmail (covered by the same `gmail.readonly` permission) and its text is extracted on this computer by nVentures itself — no AI service, no upload, no extra software. The panel then lists **facts found in the deck** — HQ, website, contact, description, stage, funding to date, current raise, founders, founding year, traction — each with the page number and the exact sentence it came from. **Use** copies a fact into the form and **Add to notes** appends the quoted line; nothing changes until you save, and reading a deck never changes a pitch's status. The full deck text is searchable (*Find in deck*). Limits, shown plainly when they apply: password-protected files, scanned decks with no text layer (no OCR), files over 20 MB, legacy .ppt and Keynote are not read; deck *links* (DocSend, Drive, …) usually need the founder's sign-in and are not fetched. Deck text is treated as data — an instruction written in a deck is never followed.

**Partners without a website.** The crawler starts from each partner's `Company Portfolio` (or `Website`) link in the partner list. When that cell is blank, nVentures looks the website up itself before crawling:
1. the domain of the partner's **Contact** email, if it's a work address (Gmail, Outlook and other free-mail domains are ignored);
2. a short list of **addresses built from the name** (e.g. *Indus Bridge Capital* → `indusbridgecapital.com`, `indusbridge.com`, `indusbridge.vc`, `.capital`, `.ventures`, regional domains);
3. only if switched on: **Tavily web search** for the partner's name.

A site is accepted only if the page itself names the partner and is about investing; parked or for-sale domains are rejected. A found website is marked **found — confirm** on the Partners page, with how it was found. Click **Confirm website** (or correct it) once you've checked it. Your partner list in the sheet is never changed. If nothing is found, the run says so, and the lookup is retried after 7 days, or sooner with **Find website**. The old app searched every partner online; without the opt-in search, a partner whose name doesn't match its domain still needs its link added.

**Outbound sourcing.** Partners are visited least-recently-sourced first. For each partner the crawler reads robots.txt and sitemaps, follows portfolio pages, pagination and RSS/Atom items, and collects official company links. Each company's own pages are fetched (bounded), and evidence is extracted as exact sentences. A company is accepted only if its pages evidence: HQ in Afghanistan, Bangladesh, Bhutan, India, Maldives, Nepal, Pakistan, Sri Lanka or Singapore; an active B2B business; bootstrapped/angel/pre-seed/seed stage; and **total** USD funding ≤ $3,000,000. Contradicting evidence → *Rejected*; missing evidence → *Needs research* (never assumed). Targets are goals; a run that cannot reach one ends *Partial*. Paste company URLs under **Research specific companies** for JavaScript-only or blocked portfolios.

**Sheet sync (both).** Before any write the app persists a sync record, reads the whole tab, and reconciles: an identical row is adopted as verified, a row for the same company is never overwritten (*Already in sheet*). New rows are appended once with RAW input, the returned row is read back in full and compared. A timeout, dropped connection or missing range is marked *Write uncertain* and the next sync reads the sheet before doing anything — appends are never blindly retried. New inbound rows leave **Status** blank; existing cells are never modified.

## Daily-use features

Light/dark/system theme without flash (labelled control in the top bar; *System* follows OS changes live), links to the verified sheet row, an *Across nVentures* panel linking the same company across pitches, sourcing and activity, job **Retry**, unsaved-edit protection, inline validation, duplicate-click protection, offline banner with automatic reconnect, clear-filters, compact density, saved per-table filters, search, sort, pagination, shortlist (star), CSV export with formula neutralisation, live job progress with cancel, job history and audit log, readiness panel with disabled actions that explain what is missing, command palette (**Ctrl/⌘ K**), shortcuts (**?**, `/`, `g` + letter, `j/k`, `x`, `s`, `t`, `c`, **Esc**), focus-trapped dialogs that return focus, mobile layout, reduced-motion support.

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
