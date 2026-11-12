from datetime import datetime, timezone
from sqlalchemy import create_engine, String, Integer, JSON, Text, ForeignKey, event
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, sessionmaker
from .config import DATABASE_URL

def now():
    return datetime.now(timezone.utc).isoformat()

class Base(DeclarativeBase):
    pass

class Company(Base):
    __tablename__ = 'companies'
    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(200))
    name_key: Mapped[str] = mapped_column(String(200), unique=True)
    domain: Mapped[str | None] = mapped_column(String(300), unique=True, nullable=True)
    linkedin_key: Mapped[str | None] = mapped_column(String(300), unique=True, nullable=True)
    data: Mapped[dict] = mapped_column(JSON)
    partner: Mapped[str] = mapped_column(String(200))
    created_at: Mapped[str] = mapped_column(String(50), default=now)
    sync_status: Mapped[str] = mapped_column(String(40), default='pending')
    sheet_row: Mapped[int | None] = mapped_column(nullable=True)
    sheet_range: Mapped[str | None] = mapped_column(String(300), nullable=True)
    sync_error: Mapped[str | None] = mapped_column(Text, nullable=True)

class Partner(Base):
    __tablename__ = 'partners'
    id: Mapped[int] = mapped_column(primary_key=True)
    name: Mapped[str] = mapped_column(String(200), unique=True)
    website: Mapped[str] = mapped_column(String(500), default='')
    relevant: Mapped[int] = mapped_column(Integer, default=1)
    last_sourced: Mapped[str] = mapped_column(String(50), default='')
    run_count: Mapped[int] = mapped_column(Integer, default=0)
    last_added: Mapped[int] = mapped_column(Integer, default=0)

class Run(Base):
    __tablename__ = 'runs'
    id: Mapped[int] = mapped_column(primary_key=True)
    started_at: Mapped[str] = mapped_column(String(50), default=now)
    finished_at: Mapped[str | None] = mapped_column(String(50), nullable=True)
    mode: Mapped[str] = mapped_column(String(20))
    status: Mapped[str] = mapped_column(String(30), default='queued')
    target: Mapped[int] = mapped_column(Integer)
    max_partners: Mapped[int] = mapped_column(Integer)
    accepted: Mapped[int] = mapped_column(Integer, default=0)
    rejected: Mapped[int] = mapped_column(Integer, default=0)
    duplicates: Mapped[int] = mapped_column(Integer, default=0)
    errors: Mapped[int] = mapped_column(Integer, default=0)
    researched: Mapped[int] = mapped_column(Integer, default=0)
    progress: Mapped[int] = mapped_column(Integer, default=0)
    message: Mapped[str] = mapped_column(Text, default='Waiting to start')
    cancel_requested: Mapped[int] = mapped_column(Integer, default=0)
    sync_to_sheets: Mapped[int] = mapped_column(Integer, default=0)
    synced: Mapped[int] = mapped_column(Integer, default=0)
    sync_errors: Mapped[int] = mapped_column(Integer, default=0)

class Event(Base):
    __tablename__ = 'events'
    id: Mapped[int] = mapped_column(primary_key=True)
    run_id: Mapped[int | None] = mapped_column(ForeignKey('runs.id'), nullable=True)
    created_at: Mapped[str] = mapped_column(String(50), default=now)
    kind: Mapped[str] = mapped_column(String(40))
    message: Mapped[str] = mapped_column(Text)
    detail: Mapped[dict] = mapped_column(JSON, default=dict)

engine = create_engine(DATABASE_URL, connect_args={'check_same_thread': False, 'timeout': 30} if DATABASE_URL.startswith('sqlite') else {})
if DATABASE_URL.startswith('sqlite'):
    @event.listens_for(engine, 'connect')
    def sqlite_settings(connection, _):
        cursor = connection.cursor()
        cursor.execute('PRAGMA foreign_keys=ON')
        cursor.execute('PRAGMA journal_mode=WAL')
        cursor.close()
Session = sessionmaker(engine, expire_on_commit=False)

def serialize(model):
    return {column.name: getattr(model, column.name) for column in model.__table__.columns}

def audit(db, kind, message, run_id=None, detail=None):
    db.add(Event(kind=kind, message=message, run_id=run_id, detail=detail or {}))
    db.commit()

