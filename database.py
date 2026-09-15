import os
import sqlite3
import hashlib
import secrets
from datetime import datetime, timezone


BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DB_PATH = os.path.join(BASE_DIR, "nventures.db")


def connect():
    conn = sqlite3.connect(DB_PATH, check_same_thread=False)
    conn.row_factory = sqlite3.Row
    return conn


def init_db():
    conn = connect()

    conn.execute("""
        CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            email TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT 'user',
            active INTEGER NOT NULL DEFAULT 1,
            created_at TEXT NOT NULL
        )
    """)

    conn.execute("""
        CREATE TABLE IF NOT EXISTS runs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_email TEXT NOT NULL,
            started_at TEXT NOT NULL,
            finished_at TEXT,
            target INTEGER NOT NULL,
            accepted_count INTEGER DEFAULT 0,
            duplicate_count INTEGER DEFAULT 0,
            rejected_count INTEGER DEFAULT 0,
            partner_error_count INTEGER DEFAULT 0,
            status TEXT NOT NULL,
            report_json TEXT
        )
    """)

    conn.commit()
    conn.close()


def hash_password(password):
    salt = secrets.token_hex(16)

    digest = hashlib.pbkdf2_hmac(
        "sha256",
        password.encode("utf-8"),
        salt.encode("utf-8"),
        200_000,
    ).hex()

    return f"{salt}${digest}"


def verify_password(password, stored_hash):
    try:
        salt, stored_digest = stored_hash.split("$", 1)

        digest = hashlib.pbkdf2_hmac(
            "sha256",
            password.encode("utf-8"),
            salt.encode("utf-8"),
            200_000,
        ).hex()

        return secrets.compare_digest(digest, stored_digest)

    except Exception:
        return False


def ensure_admin(email, password):
    if not email or not password:
        return

    email = email.strip().lower()

    conn = connect()

    existing = conn.execute(
        "SELECT id FROM users WHERE email = ?",
        (email,),
    ).fetchone()

    if existing:
        conn.close()
        return

    conn.execute(
        """
        INSERT INTO users
        (email, password_hash, role, active, created_at)
        VALUES (?, ?, ?, ?, ?)
        """,
        (
            email,
            hash_password(password),
            "admin",
            1,
            datetime.now(timezone.utc).isoformat(),
        ),
    )

    conn.commit()
    conn.close()


def authenticate(email, password):
    if not email or not password:
        return None

    email = email.strip().lower()

    conn = connect()

    row = conn.execute(
        """
        SELECT *
        FROM users
        WHERE email = ?
        AND active = 1
        """,
        (email,),
    ).fetchone()

    conn.close()

    if row is None:
        return None

    if not verify_password(password, row["password_hash"]):
        return None

    return dict(row)


def list_users():
    conn = connect()

    rows = conn.execute(
        """
        SELECT id, email, role, active, created_at
        FROM users
        ORDER BY id
        """
    ).fetchall()

    conn.close()

    return [dict(row) for row in rows]


def create_user(email, password, role="user"):
    email = email.strip().lower()

    conn = connect()

    conn.execute(
        """
        INSERT INTO users
        (email, password_hash, role, active, created_at)
        VALUES (?, ?, ?, ?, ?)
        """,
        (
            email,
            hash_password(password),
            role,
            1,
            datetime.now(timezone.utc).isoformat(),
        ),
    )

    conn.commit()
    conn.close()


def reset_password(email, new_password):
    email = email.strip().lower()

    conn = connect()

    result = conn.execute(
        """
        UPDATE users
        SET password_hash = ?
        WHERE email = ?
        """,
        (
            hash_password(new_password),
            email,
        ),
    )

    conn.commit()
    changed = result.rowcount
    conn.close()

    return changed > 0


def save_run(
    user_email,
    started_at,
    finished_at,
    target,
    report,
):
    import json

    conn = connect()

    cursor = conn.execute(
        """
        INSERT INTO runs (
            user_email,
            started_at,
            finished_at,
            target,
            accepted_count,
            duplicate_count,
            rejected_count,
            partner_error_count,
            status,
            report_json
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            user_email,
            started_at,
            finished_at,
            target,
            len(report.get("accepted", [])),
            len(report.get("duplicates", [])),
            len(report.get("rejected", [])),
            len(report.get("partner_errors", [])),
            "completed",
            json.dumps(report, default=str),
        ),
    )

    conn.commit()

    run_id = cursor.lastrowid

    conn.close()

    return run_id


def list_runs(limit=50):
    conn = connect()

    rows = conn.execute(
        """
        SELECT *
        FROM runs
        ORDER BY id DESC
        LIMIT ?
        """,
        (limit,),
    ).fetchall()

    conn.close()

    return [dict(row) for row in rows]


def get_run(run_id):
    conn = connect()

    row = conn.execute(
        "SELECT * FROM runs WHERE id = ?",
        (run_id,),
    ).fetchone()

    conn.close()

    if row is None:
        return None

    return dict(row)