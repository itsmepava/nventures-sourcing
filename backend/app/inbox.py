"""Read-only Gmail intake. Kept deliberately separate from outbound mandate rules."""
import base64
import os
import re
import threading
from email.utils import parseaddr
from urllib.parse import urlencode, quote
from urllib.request import Request, urlopen
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field
from sqlalchemy import select
from .db import Base, Session, audit, now, serialize
from sqlalchemy.orm import Mapped, mapped_column
from sqlalchemy import String, JSON, Text
from .integrations import request_json, ProviderError
from .rules import name_key, domain_key

router = APIRouter(prefix='/api/inbox')
lock = threading.Lock()
HEADERS = ['Company Name', 'Description', 'HQ', 'Contact', 'Status', 'Notes']
SHEET = ''

class Pitch(Base):
    __tablename__ = 'pitches'
    id: Mapped[int] = mapped_column(primary_key=True)
    message_id: Mapped[str] = mapped_column(String(200), unique=True)
    mode: Mapped[str] = mapped_column(String(20))
    name: Mapped[str] = mapped_column(String(300))
    status: Mapped[str] = mapped_column(String(40))
    data: Mapped[dict] = mapped_column(JSON)
    created_at: Mapped[str] = mapped_column(String(50), default=now)
    error: Mapped[str] = mapped_column(Text, default='')

class InboxJob(Base):
    __tablename__ = 'inbox_jobs'
    id: Mapped[int] = mapped_column(primary_key=True)
    status: Mapped[str] = mapped_column(String(40))
    data: Mapped[dict] = mapped_column(JSON)
    created_at: Mapped[str] = mapped_column(String(50), default=now)

def ready():
    return all(os.getenv(k) for k in ('GOOGLE_OAUTH_CLIENT_ID', 'GOOGLE_OAUTH_CLIENT_SECRET', 'GOOGLE_OAUTH_REFRESH_TOKEN'))

class Google:
    def __init__(self):
        if not ready():
            raise ProviderError('Configure Google OAuth credentials in .env first. See docs/INBOX.md.')
        payload = urlencode(dict(client_id=os.environ['GOOGLE_OAUTH_CLIENT_ID'], client_secret=os.environ['GOOGLE_OAUTH_CLIENT_SECRET'], refresh_token=os.environ['GOOGLE_OAUTH_REFRESH_TOKEN'], grant_type='refresh_token')).encode()
        try:
            import json
            with urlopen(Request('https://oauth2.googleapis.com/token', data=payload), timeout=30) as response:
                self.token = json.load(response)['access_token']
        except Exception:
            raise ProviderError('Google authorization failed. Reconnect the Google account.') from None
        profile = self.call('gmail/v1/users/me/profile')
        if profile['emailAddress'].lower() != os.getenv('GMAIL_EXPECTED_ACCOUNT', 'contact@nventures.sg').lower():
            raise ProviderError('Connected Gmail account does not match GMAIL_EXPECTED_ACCOUNT.')

    def call(self, path, payload=None, write=False):
        # An append is attempted once; uncertain writes are reconciled on the next run.
        return request_json('https://www.googleapis.com/' + path, payload, {'Authorization': 'Bearer ' + self.token}, attempts=1 if write else 4)

    def rows(self):
        tab = os.getenv('INBOX_SHEET_TAB', 'Sheet1').replace("'", "''")
        self.range = quote("'" + tab + "'!A:F", safe='')
        self.sheet = os.getenv('INBOX_SPREADSHEET_ID', SHEET)
        if not self.sheet:
            raise ProviderError('Configure INBOX_SPREADSHEET_ID before syncing pitches.')
        rows = self.call(f'sheets/v4/spreadsheets/{self.sheet}/values/{self.range}').get('values', [])
        if not rows or rows[0] != HEADERS:
            raise ProviderError('Inbound sheet must have the six expected headers. No cells were changed.')
        return rows

    def sync(self, data):
        rows = self.rows()
        row = [data.get(k, '') for k in ('name', 'description', 'hq', 'contact')] + ['', data.get('notes', '')]
        for index, existing in enumerate(rows[1:], 2):
            if existing and name_key(existing[0]) == name_key(data['name']):
                return 'verified' if (existing + [''] * 6)[:6] == row else 'already_exists', index
            website = data.get('website', '')
            host = domain_key(website) if website else None
            if host and host not in ('linkedin.com', 'drive.google.com', 'docs.google.com', 'facebook.com', 'instagram.com', 'docsend.com', 'x.com') and re.search(r'(?<![\w.-])' + re.escape(host) + r'(?![\w.-])', ' '.join(existing).lower()):
                return 'already_exists', index
        result = self.call(f'sheets/v4/spreadsheets/{self.sheet}/values/{self.range}:append?valueInputOption=RAW&insertDataOption=INSERT_ROWS', {'values': [row]}, write=True)
        written = result.get('updates', {}).get('updatedRange', '')
        if not written:
            raise ProviderError('Write outcome uncertain. Run again to reconcile before appending.')
        actual = self.call(f'sheets/v4/spreadsheets/{self.sheet}/values/{quote(written, safe="")}').get('values', [[]])[0]
        if (actual + [''] * 6)[:6] != row:
            raise ProviderError('Sheet read-back did not match. Inspect the sheet before retrying.')
        return 'verified', int(re.search(r'!A(\d+)', written).group(1))

def body_text(payload):
    content = []
    if payload.get('mimeType') == 'text/plain' and payload.get('body', {}).get('data'):
        raw = payload['body']['data']
        content.append(base64.urlsafe_b64decode(raw + '=' * (-len(raw) % 4)).decode('utf-8', errors='replace'))
    for part in payload.get('parts', []):
        content.append(body_text(part))
    return '\n'.join(content)

LABELS = ['Full name', 'Company name', 'Website', 'Stage', 'Sector', 'Country of incorporation', 'One line on what you do', 'Link to deck (DocSend / Drive)', 'Contact email', 'How did you hear about us?']
def extract(text, sender='', subject=''):
    pattern = r'(?im)^\s*(' + '|'.join(re.escape(x) for x in LABELS) + r')\s*:?\s*\n?'
    matches = list(re.finditer(pattern, text))
    fields = {m.group(1).lower(): text[m.end():matches[i + 1].start() if i + 1 < len(matches) else len(text)].strip() for i, m in enumerate(matches)}
    name = fields.get('company name', '')[:300]
    description = fields.get('one line on what you do', '')[:12000]
    contact = fields.get('contact email', '') or parseaddr(sender)[1]
    notes = '\n'.join(f'{key}: {value}' for key, value in fields.items() if key not in ('company name', 'one line on what you do', 'contact email'))
    notes += '\nSender-reported. HQ unconfirmed; country of incorporation is not HQ. Linked decks have not been reviewed.'
    status = 'ready' if name and len(description) >= 35 and '@' in contact else 'review'
    if re.search(r'\b(publicly traded|multinational|investment fund|investment firm|newsletter|established in 19\d\d)\b', description, re.I):
        status = 'review'
    if name_key(name) in ('test', 'testcompany', 'ts') and re.search(r'\btest\b', description, re.I):
        status = 'skipped'
    return dict(name=name or subject[:200] or 'Unstructured pitch', description=description or text[:12000], contact=contact, hq='', website=fields.get('website', ''), notes=notes, status=status)

class Start(BaseModel):
    demo: bool = True
    sync: bool = True
    limit: int = Field(100, ge=1, le=500)

def execute(job_id, options):
    try:
        google = None if options.demo else Google()
        with Session() as db:
            job = db.get(InboxJob, job_id)
            counts = dict(processed=0, verified=0, review=0, skipped=0, duplicates=0, errors=0, message='Starting', demo=options.demo)
            ids, next_page = [], None
            if options.demo:
                ids = ['demo-form-1', 'demo-direct-1']
            else:
                query = os.getenv('GMAIL_PITCH_QUERY', '-in:spam -in:trash -in:sent {"Company name:" "pitch deck" subject:fundraising subject:pitch}')
                prefix = os.getenv('GMAIL_EXPECTED_ACCOUNT', 'contact@nventures.sg') + ':'
                saved = db.scalars(select(Pitch).where(Pitch.mode == 'live')).all()
                done = {p.message_id for p in saved if p.status not in ('ready', 'error')}
                ids = [p.message_id[len(prefix):] for p in saved if p.status in ('ready', 'error') and p.message_id.startswith(prefix)][:options.limit]
                pages = 0
                while len(ids) < options.limit:
                    result = google.call('gmail/v1/users/me/messages?' + urlencode(dict(q=query, maxResults=100, **({'pageToken': next_page} if next_page else {}))))
                    candidates = [m['id'] for m in result.get('messages', []) if prefix + m['id'] not in done and m['id'] not in ids]
                    remaining = options.limit - len(ids)
                    ids += candidates[:remaining]
                    next_page = result.get('nextPageToken')
                    pages += 1
                    if len(candidates) > remaining or pages >= 20:
                        next_page = next_page or 'bounded-scan'
                        break
                    if not next_page:
                        break
            counts['more_available'] = bool(next_page)
            for i, message_id in enumerate(ids):
                job = db.get(InboxJob, job_id)
                db.refresh(job)
                if job.status == 'cancelled':
                    break
                key = ('demo:' if options.demo else os.getenv('GMAIL_EXPECTED_ACCOUNT', 'contact@nventures.sg') + ':') + message_id
                pitch = db.scalar(select(Pitch).where(Pitch.message_id == key))
                try:
                    if pitch and pitch.status not in ('error', 'ready'):
                        counts['duplicates'] += 1
                        continue
                    if not pitch:
                        if options.demo:
                            text = 'Company name: Example Climate Labs\nWebsite: https://example.com\nOne line on what you do: Software helping small businesses track energy consumption and reduce waste.\nContact email: founder@example.com\nCountry of incorporation: Singapore' if i == 0 else 'We would like to share our idea. Can we meet?'
                            data = extract(text, 'founder@example.com', 'Pitch for review')
                        else:
                            msg = google.call(f'gmail/v1/users/me/messages/{message_id}?format=full')
                            headers = {h['name'].lower(): h['value'] for h in msg['payload'].get('headers', [])}
                            data = extract(body_text(msg['payload']), headers.get('reply-to', headers.get('from', '')), headers.get('subject', ''))
                            data['gmail_url'] = f'https://mail.google.com/mail/u/0/#all/{msg["threadId"]}'
                            data['notes'] += '\nSource message: ' + message_id
                        # Unknown or unstructured businesses require human review; no model decides eligibility silently.
                        pitch = Pitch(message_id=key, mode='demo' if options.demo else 'live', name=data['name'], status=data['status'], data=data)
                        db.add(pitch)
                        db.commit()
                    if pitch.status in ('ready', 'error') and options.sync:
                        if google:
                            status, row = google.sync(pitch.data)
                            pitch.status, pitch.data = status, {**pitch.data, 'sheet_row': row}
                        else:
                            pitch.status = 'demo_verified'
                        counts['verified'] += 1
                    elif pitch.status in counts:
                        counts[pitch.status] += 1
                    pitch.error = ''
                    db.commit()
                except Exception:
                    db.rollback()
                    if pitch and pitch.id:
                        pitch.status, pitch.error = 'error', 'Import or verification failed. Saved data retained; retry will reconcile the sheet.'
                        db.commit()
                    counts['errors'] += 1
                finally:
                    counts['processed'] += 1
                    counts['message'] = f'Processed {i + 1} of {len(ids)} messages'
                    job.data = dict(counts)
                    db.commit()
            if job.status != 'cancelled':
                job.status = 'completed_with_errors' if counts['errors'] else 'completed'
            audit(db, 'inbox_run', f'Inbox run {job.id}: {job.status}', detail=counts)
    except Exception as exc:
        with Session() as db:
            job = db.get(InboxJob, job_id)
            job.status, job.data = 'failed', {'message': str(exc) if isinstance(exc, ProviderError) else 'Inbox run failed. Check Google configuration.'}
            audit(db, 'inbox_error', job.data['message'])
    finally:
        lock.release()

@router.get('')
def overview():
    with Session() as db:
        return dict(ready=ready(), account=os.getenv('GMAIL_EXPECTED_ACCOUNT', 'contact@nventures.sg'), sheet=os.getenv('INBOX_SPREADSHEET_ID', SHEET), pitches=[serialize(p) for p in db.scalars(select(Pitch).order_by(Pitch.id.desc()))], runs=[serialize(r) for r in db.scalars(select(InboxJob).order_by(InboxJob.id.desc()).limit(20))])

class Review(BaseModel):
    name: str = Field(min_length=1, max_length=300)
    description: str = Field(min_length=10, max_length=12000)
    contact: str = Field(max_length=500)
    hq: str = Field(default='', max_length=300)
    notes: str = Field(default='', max_length=12000)
    decision: str = 'ready'

@router.post('/pitches/{pitch_id}')
def review(pitch_id: int, values: Review):
    if values.decision not in ('ready', 'skipped'):
        raise HTTPException(400, 'Choose ready or skipped')
    with Session() as db:
        pitch = db.get(Pitch, pitch_id)
        if not pitch:
            raise HTTPException(404, 'Pitch not found')
        if pitch.status in ('verified', 'already_exists', 'demo_verified'):
            raise HTTPException(400, 'Synced records cannot be changed here.')
        pitch.data = {**pitch.data, **values.model_dump(exclude={'decision'})}
        pitch.name, pitch.status = values.name, values.decision
        audit(db, 'inbox_review', f'Pitch {pitch.id}: {values.decision}')
        return serialize(pitch)

@router.post('/runs')
def start(options: Start):
    if not options.demo and not ready():
        raise HTTPException(400, 'Connect Google using the setup instructions before a live import.')
    if not lock.acquire(blocking=False):
        raise HTTPException(409, 'An inbox import is already running.')
    try:
        with Session() as db:
            job = InboxJob(status='running', data={'demo': options.demo, 'message': 'Starting'})
            db.add(job)
            db.commit()
            result = serialize(job)
            threading.Thread(target=execute, args=(job.id, options), daemon=True).start()
            return result
    except Exception:
        lock.release()
        raise

@router.post('/runs/{job_id}/cancel')
def cancel(job_id: int):
    with Session() as db:
        job = db.get(InboxJob, job_id)
        if not job:
            raise HTTPException(404, 'Run not found')
        if job.status == 'running':
            job.status = 'cancelled'
            db.commit()
        return serialize(job)
