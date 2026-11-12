"""Copy a stopped local workspace into an empty, migrated PostgreSQL database.

Connection strings are read from env; never logged. Also supports SQLite for tests.
"""
import os
from sqlalchemy import create_engine, select, func, text
from app.db import Company, Partner, Run, Event

def transfer(source_url, target_url):
    source = create_engine(source_url)
    target = create_engine(target_url)
    if source_url == target_url:
        raise ValueError('Source and target must be different databases')
    tables = [Partner.__table__, Company.__table__, Run.__table__, Event.__table__]
    counts = {}
    try:
        with source.connect() as src, target.begin() as dst:
            if any(dst.scalar(select(func.count()).select_from(t)) for t in tables):
                raise ValueError('Target is not empty; migration stopped without overwriting data')
            for table in tables:
                rows = [dict(row) for row in src.execute(select(table)).mappings()]
                if rows:
                    dst.execute(table.insert(), rows)
                counts[table.name] = len(rows)
                if dst.scalar(select(func.count()).select_from(table)) != len(rows):
                    raise ValueError('Row count verification failed')
                if target.dialect.name == 'postgresql' and rows:
                    dst.execute(text("SELECT setval(pg_get_serial_sequence(:table, 'id'), :last, true)"), {'table': table.name, 'last': max(row['id'] for row in rows)})
        return counts
    finally:
        source.dispose(); target.dispose()

if __name__ == '__main__':
    print(transfer(os.environ['SOURCE_DATABASE_URL'], os.environ['TARGET_DATABASE_URL']))
