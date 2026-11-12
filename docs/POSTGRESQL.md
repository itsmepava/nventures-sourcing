# Optional PostgreSQL migration

No PostgreSQL installation is required for localhost. This pathway keeps the same application and model code.

1. Stop sourcing and both local servers. Back up the SQLite database, including WAL files if present, after all connections have closed.
2. Provision a new empty PostgreSQL database. Install the optional driver with `.\.venv\Scripts\python.exe -m pip install -r backend\requirements-postgres.txt`.
3. Set `DATABASE_URL=postgresql+psycopg://...` securely in the environment. From `backend/`, run `..\.venv\Scripts\python.exe -m alembic upgrade head`. This creates the versioned schema without starting the app or seeding demo content.
4. Set `SOURCE_DATABASE_URL` to the absolute SQLite URL and `TARGET_DATABASE_URL` to the same PostgreSQL URL. Run `..\.venv\Scripts\python.exe transfer_database.py` from `backend/`. Credentials are read from the environment and are not printed. The script refuses a target with any existing app rows; copies companies, partners, runs and events in one target transaction; preserves IDs; verifies row counts; resets PostgreSQL sequences. A failure rolls back the target copy. The source is not changed.
5. Start the backend with the target URL, inspect counts/profiles/history, run regression tests in an isolated test DB, and do a small sourcing/sync smoke test. Keep the original SQLite backup until verified.

Do not pass database credentials as command-line arguments or commit them. The copy script has been verified SQLite-to-SQLite with the same schema; an actual PostgreSQL server has not been exercised in this build.

To make future schema changes, update the ORM models and generate a new Alembic revision with `python -m alembic revision --autogenerate -m "description"` from `backend/`; review the generated migration before applying it. Keep `0001_initial.py` immutable once released. This build ships a frozen initial schema and a revision template.
