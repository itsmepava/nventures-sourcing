# Streamlit migration review

The supplied Alpha ZIP was inspected without running its app or exposing API keys. It contains `app.py`, auth/config/database modules, an importable sourcing engine, Google Sheets integration, logo asset, and two SQLite databases.

The README describes Tavily and strict screening, but the actual engine uses keyless FreeSerp and its prompts expressly allow any HQ, funding amount, and stage. The user's current mandate takes precedence; those permissive prompts were not copied into 2.0.

| Behavior | 2.0 treatment |
| --- | --- |
| Least recently sourced partner; relevance flag | Preserved in database rotation, with sheet control state read when configured |
| Normalize company legal suffix/domain/LinkedIn | Preserved and strengthened with database unique constraints |
| Structured research fields | Preserved, including legacy sheet aliases |
| Missing-field enrichment | Targeted second search for HQ, funding, founders, LinkedIn, team and customers |
| Immediate accepted-company persistence | Local database commit immediately; explicit outbound sync |
| Per-partner/candidate failure isolation | Preserved with persisted audit events |
| Sheets physical insert and read-back | Append with INSERT_ROWS and RAW; actual returned range; entire row verified; no guessed row count |
| Google service account env/base64 JSON | Preserved, lazily loaded optional integration |
| FreeSerp | Replaced with requested Tavily, no Tavily SDK |
| Permissive geography/stage/funding | Replaced with current deterministic hard mandate |
| Streamlit session and user tables | UI removed; no copying old users/passwords into a localhost-only app |
| Run history | New persisted run schema; optional counts-only legacy import |
| Worldwide Sri Lankan founder sourcing | Excluded because founder nationality does not satisfy eligible HQ |
| Automatic partner discovery | Not part of this build; add partners or use existing partner sheet |

## Optional legacy run history import

Start the backend once so migrations create the 2.0 database, then stop it. In `backend/`, using the project virtual environment:

```powershell
..\.venv\Scripts\python.exe import_legacy_history.py 'C:\path\to\legacy\nventures.db'
# Inspect the dry-run count, then explicitly import:
..\.venv\Scripts\python.exe import_legacy_history.py 'C:\path\to\legacy\nventures.db' --apply
```

The source connection is read-only. Only run timestamps/targets/counts are copied, labelled `legacy` / `imported`; no users, password hashes, API response bodies, or historical companies. Repeated import of the same absolute source path + old run ID is skipped. Do not treat historical accepted totals as evidence the old companies satisfy the new mandate. Old and new schemas are separate; never point `DATABASE_URL` directly at the old DB.
