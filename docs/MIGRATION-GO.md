# Moving from the Python app to the Go app

The Go app never modifies the Python app, its `.env` or its databases. Migration copies records into the Go app's own database.

## What is imported

| Python table | Go destination | Notes |
|---|---|---|
| `companies` | `companies` | All columns incl. full JSON `data` (38-column fields, evidence, sources), `sheet_row`, `sheet_range`, `sync_status`, `sync_error`, `created_at` |
| `partners` | `partners` | Name, website, relevance, last sourced, run count, last added |
| `runs` | `jobs` (kind `outbound_run`) | Counters (accepted, rejected, duplicates, errors, researched, synced → verified, sync_errors → sync_failed), message, cancel flag; `running/queued` become `interrupted` |
| `inbox_jobs` | `jobs` (kind `inbound_import`) | Counters JSON kept; `running` becomes `interrupted` |
| `events` | `events` | Linked to the migrated run when it has one |
| `pitches` | `pitches` | Message key, mode, edited name, status (incl. legacy `demo_verified`), full data and error |
| `alembic_version` | report | Must be 0001–0003 |

Every imported row has a `legacy_ref` (`py:<table>:<id>`), so running the import again imports nothing new. A row whose company identity already exists in the Go database is kept as the existing record and listed under *skipped* in the report.

## Steps

1. Stop nothing. The Python app may keep running; the source is opened read-only.
2. Take a consistent copy of the Python database (recommended; the importer can also read the live file read-only):
   ```bash
   python3 -c "import sqlite3,sys; s=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True); d=sqlite3.connect(sys.argv[2]); s.backup(d)" outputs/nventures-sourcing-2/backend/nventures.db /tmp/python-copy.db
   ```
3. Import into the matching mode (demo records only into demo, live only into live):
   ```bash
   ./bin/nventures migrate-python --from /tmp/python-copy.db --mode live --report migration-report.json
   ```
4. Read the report: `source_unchanged` must be `true` and each check must end in `ok`. If the Go database already had data, `target_backup` names the automatic pre-migration backup.

## Rollback

- The Python app and its files were never changed: keep using `sh start.sh`.
- To undo the import in the Go app: stop it, then either restore `target_backup` over the Go database file, or delete the Go database (`~/.local/share/nventures/<mode>/nventures-demo.db` or `nventures.db`) to start empty.
- To roll back the Go app entirely, stop using `start-go.sh`; nothing else depends on it.

## Verified on 12 November 2026

Imported a consistent copy of the recovered Python database (alembic 0003: 6 companies, 6 partners, 1 run, 3 inbox jobs, 10 events, 2 pitches) and, separately, the live file read-only: all counts matched, a field-by-field comparison found 0 mismatches, the repeat import added nothing, and SHA-256 hashes of the Python `.env` files, `nventures.db` and `nventures.db-wal` were identical before and after. A fictional fixture with every table, interrupted jobs, sync errors, sheet ranges, edited pitches and Unicode/newline data is covered by `go test ./internal/store`.
