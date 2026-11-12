"""Read-only access to the legacy DB; import run counts only, never users/keys.
Default is dry-run. Does not modify the original file.
"""
import argparse
import sqlite3
from contextlib import closing
from pathlib import Path
from sqlalchemy import select
from app.db import Session, Run, Event, audit

def import_history(path, apply=False):
    uri = Path(path).resolve().as_uri() + '?mode=ro'
    with closing(sqlite3.connect(uri, uri=True)) as legacy:
        legacy.row_factory = sqlite3.Row
        rows = [dict(r) for r in legacy.execute('SELECT * FROM runs ORDER BY id')]
    print(f'Found {len(rows)} legacy run records. Users and passwords will not be copied.')
    if not apply:
        print('Dry-run only. Add --apply to import into the configured 2.0 database.')
        return
    imported = 0
    with Session() as db:
        for row in rows:
            key = str(Path(path).resolve()) + ':' + str(row['id'])
            if any(e.detail.get('legacy_key') == key for e in db.scalars(select(Event).where(Event.kind == 'legacy_import'))):
                continue
            run = Run(started_at=row['started_at'], finished_at=row.get('finished_at'), mode='legacy', status='imported', target=row['target'], max_partners=0, accepted=row.get('accepted_count') or 0, rejected=row.get('rejected_count') or 0, duplicates=row.get('duplicate_count') or 0, errors=row.get('partner_error_count') or 0, progress=100, message='Imported legacy run totals; historical companies were not re-qualified')
            db.add(run); db.commit()
            audit(db, 'legacy_import', 'Legacy run totals imported', run.id, {'legacy_key': key})
            imported += 1
    print(f'Imported {imported} runs. Original database unchanged.')

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('database')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    import_history(args.database, args.apply)
