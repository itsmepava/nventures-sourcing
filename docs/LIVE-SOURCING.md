# Real sourcing and spreadsheet writes

Demo mode does not find real companies or update a spreadsheet. It uses synthetic fixtures to exercise the interface and rules. Live mode uses Tavily for web discovery, OpenRouter for evidence extraction, deterministic qualification, the local database, and Google Sheets.

## Your installed copy

At the time of this update the installed Downloads copy had `APP_MODE=demo`, `SHEETS_SYNC_ENABLED=false`, and no OpenRouter key, Tavily key, Google service-account JSON, or spreadsheet ID. This is why it was not doing the original tool's real sourcing work.

## One-time live configuration

Stop the app. In its folder, install the optional Sheets support:

```powershell
.\.venv\Scripts\python.exe -m pip install -r backend\requirements-sheets.txt
notepad .env
```

Keep all credentials local. Fill in:

```dotenv
APP_MODE=live
DATABASE_URL=sqlite:///./nventures-live.db
OPENROUTER_API_KEY=<your key>
OPENROUTER_MODELS=openrouter/free
TAVILY_API_KEY=<your key>
GOOGLE_SERVICE_ACCOUNT_JSON=<single-line JSON or base64 JSON>
SPREADSHEET_ID=<your actual spreadsheet ID>
SOURCING_TAB_NAME=Active Sourcing
PARTNER_TAB_NAME=Partner List
CONTROL_TAB_NAME=Sourcing Control
SHEETS_SYNC_ENABLED=true
```

The Google Sheets API must be enabled, and the sheet shared with the service-account email as Editor. Use the actual tab names if yours differ. The partner tab expects a `Company Name` header; the control tab expects `Partner Name`, `Last Sourced UTC`, `Run Count`, `Last Run Added`. The existing company sheet headers, including legacy typos and repeated notes columns, determine output order.

Restart with `Start.cmd`. Settings distinguishes key presence from connection verification. Click **Test live connections**: OpenRouter key read, one small Tavily search, and a Google sheet read. No spreadsheet rows are written by this test. A successful read does not independently prove write access.

## Original sourcing workflow restored

Start a small live run with **Write accepted companies to Google Sheets** checked. That option is selected by default when sheet configuration is ready, and unavailable in demo or incomplete configuration.

1. Load existing company identities and partner rotation from the sheet.
2. Discover partner portfolio companies and deeply research them.
3. Reject unsupported HQ, non-B2B, inactive, later-stage, unknown/over-limit funding, and duplicates.
4. Save each accepted company locally immediately.
5. Write it to the sheet and read back every cell.
6. Record the actual row/range and increase the run's verified-write count.
7. Update partner control state and keep going after individual errors.

Accepted count and verified sheet-write count are separate. A sync failure does not erase the locally saved company or claim a successful sheet write. Inspect its profile/audit event, fix the underlying connection, then use **Sync qualified companies** to retry pending/error rows. Retries reconcile existing rows before appending again.

With the checkbox off, accepted companies remain local and can be synced manually later. A cancelled run retains previously saved/written companies.

## What has and has not been verified

The complete immediate-write pipeline is tested with fake provider/worksheet objects, including successful row verification, sync failures, and company retention. Existing regression tests also cover real adapter parsing, retry behavior, duplicate checks and qualification. No live API keys were used and no actual spreadsheet was modified during development. Your configured connection test and a small live sourcing run are still required before treating the integration as verified for your spreadsheet.

## Updating your existing installation

An update should preserve `.env`, `.venv`, and every `*.db`/WAL file. Schema migration `0002` adds sheet-write counters to existing run history at startup. Do not replace your database with a bundled file; the source ZIP contains none.
