import csv
import io
import os
import threading
from contextlib import asynccontextmanager
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import Response
from pydantic import BaseModel, Field
from sqlalchemy import select, func
from . import config, demo, integrations
from .db import Session, Company, Partner, Run, Event, audit, now, serialize
from .engine import operation_lock, execute, safe_error, sync_company
from .rules import COUNTRIES, qualifies

@asynccontextmanager
async def lifespan(app):
    # Apply versioned migrations before starting; never mutate schema implicitly.
    from alembic.config import Config
    from alembic import command
    cfg = Config(str(config.ROOT / 'backend' / 'alembic.ini'))
    command.upgrade(cfg, 'head')
    from .inbox import InboxJob
    with Session() as db:
        for job in db.scalars(select(InboxJob).where(InboxJob.status == 'running')):
            job.status = 'interrupted'
        db.commit()
        for c in db.scalars(select(Company)).all():
            if bool(c.data.get('demo')) != (config.MODE == 'demo'):
                raise RuntimeError('Database contains records from another mode. Use a separate DATABASE_URL for demo and live.')
        for run in db.scalars(select(Run).where(Run.status.in_(['running', 'queued']))).all():
            run.status, run.finished_at, run.message = 'interrupted', now(), 'Server restarted. Saved companies retained; start a new run.'
            audit(db, 'interrupted', run.message, run.id)
        if config.MODE == 'demo' and not db.scalar(select(func.count()).select_from(Partner)):
            db.add_all(Partner(name=n, website=w) for n, w in demo.PARTNERS)
            db.commit()
        if config.MODE == 'demo' and not db.scalar(select(func.count()).select_from(Company)):
            from .rules import name_key, domain_key
            for i in range(6):
                record = demo.candidate(i, demo.PARTNERS[i % len(demo.PARTNERS)][0])
                db.add(Company(name=record['name'], name_key=name_key(record['name']), domain=domain_key(record['website']), data=record, partner=record['investors'], sync_status='demo'))
            db.commit()
    yield

app = FastAPI(title='nVentures Sourcing 2.0', lifespan=lifespan)
app.add_middleware(CORSMiddleware, allow_origins=['http://127.0.0.1:5173', 'http://localhost:5173'], allow_methods=['GET', 'POST', 'PATCH'], allow_headers=['Content-Type'])

@app.middleware('http')
async def local_only(request: Request, call_next):
    # Prevent browser sites on other origins from invoking local mutations.
    if request.method not in ('GET', 'HEAD', 'OPTIONS'):
        origin = request.headers.get('origin')
        if origin and origin not in ('http://127.0.0.1:5173', 'http://localhost:5173', 'http://127.0.0.1:8000', 'http://localhost:8000'):
            return Response('Origin not allowed', status_code=403)
    host = request.headers.get('host', '').split(':')[0]
    if host not in ('127.0.0.1', 'localhost', 'testserver'):
        return Response('Localhost only', status_code=403)
    return await call_next(request)

@app.get('/api/health')
def health():
    return {'status': 'ok', 'mode': config.MODE, 'app': 'nventures-sourcing-2'}

@app.get('/api/settings')
def settings():
    import importlib.util
    configured = {key: bool(os.getenv(key)) for key in ['OPENROUTER_API_KEY', 'TAVILY_API_KEY', 'GOOGLE_SERVICE_ACCOUNT_JSON', 'SPREADSHEET_ID']}
    sheets_installed = importlib.util.find_spec('gspread') is not None
    return {'mode': config.MODE, 'sheets_enabled': config.SYNC_ENABLED, 'sheets_installed': sheets_installed, 'integrations': configured, 'live_ready': config.MODE == 'live' and configured['OPENROUTER_API_KEY'] and configured['TAVILY_API_KEY'], 'sheets_ready': config.SYNC_ENABLED and configured['GOOGLE_SERVICE_ACCOUNT_JSON'] and configured['SPREADSHEET_ID'] and sheets_installed, 'sourcing_tab': os.getenv('SOURCING_TAB_NAME', 'Active Sourcing'), 'partner_tab': os.getenv('PARTNER_TAB_NAME', 'Partner List'), 'models': os.getenv('OPENROUTER_MODELS', 'openrouter/free'), 'countries': COUNTRIES, 'funding_limit': 3000000, 'max_research': config.MAX_RESEARCH}

@app.post('/api/integrations/check')
def check_integrations():
    if config.MODE != 'live':
        raise HTTPException(400, 'Demo mode never calls external services. Configure .env for live mode and restart first.')
    results = []
    checks = [('OpenRouter', lambda: integrations.request_json('https://openrouter.ai/api/v1/key', headers={'Authorization': 'Bearer ' + os.getenv('OPENROUTER_API_KEY', '')})), ('Tavily', lambda: integrations.search('nVentures venture capital Singapore official website')), ('Google Sheets', lambda: integrations.Sheets().snapshot())]
    for name, check in checks:
        try:
            check()
            results.append({'name': name, 'status': 'connected', 'message': 'Read test passed. No sheet rows written.'})
        except Exception as exc:
            results.append({'name': name, 'status': 'error', 'message': safe_error(exc)})
    with Session() as db:
        audit(db, 'connection_check', 'Live connection checks completed', detail={'results': results})
    return {'results': results}

@app.get('/api/dashboard')
def dashboard():
    with Session() as db:
        companies = db.scalars(select(Company)).all()
        runs = db.scalars(select(Run).order_by(Run.id.desc()).limit(10)).all()
        by_country = {c: sum(x.data.get('hq_country') == c for x in companies) for c in COUNTRIES}
        return {'companies': len(companies), 'partners': db.scalar(select(func.count()).select_from(Partner)), 'runs': db.scalar(select(func.count()).select_from(Run)), 'verified_syncs': sum(c.sync_status == 'verified' for c in companies), 'by_country': by_country, 'recent_runs': [serialize(r) for r in runs], 'events': [serialize(e) for e in db.scalars(select(Event).order_by(Event.id.desc()).limit(12)).all()]}

@app.get('/api/companies')
def companies(q: str = '', country: str = '', stage: str = ''):
    with Session() as db:
        items = db.scalars(select(Company).order_by(Company.id.desc())).all()
        return [serialize(c) for c in items if (not q or q.casefold() in (c.name + ' ' + c.partner + ' ' + c.data.get('sector', '')).casefold()) and (not country or c.data.get('hq_country') == country) and (not stage or c.data.get('stage') == stage)]

@app.get('/api/companies/export')
def export():
    buf = io.StringIO()
    writer = csv.writer(buf)
    writer.writerow(['Company Name', 'Country', 'Sector', 'Stage', 'Total Funding USD', 'Website', 'Partner', 'Sheet Row', 'Sync Status'])
    with Session() as db:
        for c in db.scalars(select(Company)).all():
            row = [c.name, c.data.get('hq_country', ''), c.data.get('sector', ''), c.data.get('stage', ''), c.data.get('total_funding_usd', ''), c.data.get('website', ''), c.partner, c.sheet_row or '', c.sync_status]
            writer.writerow(["'" + x if isinstance(x, str) and x.startswith(('=', '+', '-', '@')) else x for x in row])
    return Response(buf.getvalue(), media_type='text/csv', headers={'Content-Disposition': 'attachment; filename=nventures-companies.csv'})

@app.get('/api/companies/{company_id}')
def company(company_id: int):
    with Session() as db:
        item = db.get(Company, company_id)
        if not item:
            raise HTTPException(404, 'Company not found')
        return serialize(item)

class RunInput(BaseModel):
    target: int = Field(default=10, ge=1, le=100)
    max_partners: int = Field(default=6, ge=1, le=30)
    sync_to_sheets: bool = False

@app.post('/api/runs', status_code=202)
def start_run(body: RunInput):
    if config.MODE == 'live' and (not os.getenv('OPENROUTER_API_KEY') or not os.getenv('TAVILY_API_KEY')):
        raise HTTPException(400, 'Configure OpenRouter and Tavily environment variables first')
    if body.sync_to_sheets and not settings()['sheets_ready']:
        raise HTTPException(400, 'Set live mode, configure Google credentials and spreadsheet ID, enable SHEETS_SYNC_ENABLED, and install requirements-sheets.txt before writing to Sheets')
    if not operation_lock.acquire(blocking=False):
        raise HTTPException(409, 'A sourcing or sync operation is already running')
    try:
        with Session() as db:
            run = Run(target=body.target, max_partners=body.max_partners, mode=config.MODE, sync_to_sheets=int(body.sync_to_sheets))
            db.add(run)
            db.commit()
            result = serialize(run)
            audit(db, 'started', f'{config.MODE.title()} run started; target {body.target}', run.id)
        threading.Thread(target=execute, args=(result['id'],), daemon=True).start()
        return result
    except Exception:
        operation_lock.release()
        raise

@app.get('/api/runs')
def runs():
    with Session() as db:
        return [serialize(r) for r in db.scalars(select(Run).order_by(Run.id.desc())).all()]

@app.get('/api/runs/{run_id}')
def run(run_id: int):
    with Session() as db:
        item = db.get(Run, run_id)
        if not item:
            raise HTTPException(404, 'Run not found')
        return serialize(item) | {'events': [serialize(e) for e in db.scalars(select(Event).where(Event.run_id == run_id).order_by(Event.id)).all()]}

@app.post('/api/runs/{run_id}/cancel')
def cancel(run_id: int):
    with Session() as db:
        item = db.get(Run, run_id)
        if not item:
            raise HTTPException(404, 'Run not found')
        if item.status in ('queued', 'running'):
            item.cancel_requested = 1
            audit(db, 'cancel', 'Cancellation requested; current network request may finish first', run_id)
        return serialize(item)

@app.get('/api/partners')
def partners():
    with Session() as db:
        return [serialize(p) for p in db.scalars(select(Partner).order_by(Partner.last_sourced, Partner.id)).all()]

class PartnerInput(BaseModel):
    name: str = Field(min_length=2, max_length=200)
    website: str = Field(default='', max_length=500)

@app.post('/api/partners', status_code=201)
def add_partner(body: PartnerInput):
    from sqlalchemy.exc import IntegrityError
    with Session() as db:
        if len(body.name.strip()) < 2:
            raise HTTPException(422, 'Partner name must contain at least two characters')
        item = Partner(name=body.name.strip(), website=body.website.strip())
        db.add(item)
        try:
            db.commit()
        except IntegrityError:
            raise HTTPException(409, 'Partner already exists') from None
        audit(db, 'partner_added', f'Partner added: {item.name}')
        return serialize(item)

@app.patch('/api/partners/{partner_id}')
def toggle_partner(partner_id: int):
    with Session() as db:
        item = db.get(Partner, partner_id)
        if not item:
            raise HTTPException(404, 'Partner not found')
        item.relevant = 1 - item.relevant
        audit(db, 'partner_updated', f'{item.name}: ' + ('included' if item.relevant else 'excluded'))
        return serialize(item)

@app.get('/api/audit')
def events(after: int = 0, kind: str = ''):
    with Session() as db:
        query = select(Event).where(Event.id > after)
        if kind:
            query = query.where(Event.kind == kind)
        return [serialize(e) for e in db.scalars(query.order_by(Event.id.desc()).limit(500)).all()]

@app.post('/api/sync')
def sync():
    if not config.SYNC_ENABLED:
        raise HTTPException(400, 'Sheets sync is disabled; demo never calls external services')
    if not operation_lock.acquire(blocking=False):
        raise HTTPException(409, 'Wait for the current operation to finish')
    try:
        sheets = integrations.Sheets()
        results = []
        with Session() as db:
            items = db.scalars(select(Company).where(Company.sync_status.in_(['pending', 'error']))).all()
            for c in items:
                if c.data.get('demo'):
                    continue
                sync_company(db, sheets, c)
                results.append({'id': c.id, 'status': c.sync_status, 'row': c.sheet_row})
        return {'results': results}
    except integrations.ProviderError as exc:
        raise HTTPException(400, str(exc)) from None
    finally:
        operation_lock.release()

from .inbox import router as inbox_router
app.include_router(inbox_router)

# The built React interface can be served by the same Python process as the API.
frontend_dist = config.ROOT / 'frontend' / 'dist'
if (frontend_dist / 'index.html').is_file():
    from fastapi.staticfiles import StaticFiles
    app.mount('/', StaticFiles(directory=frontend_dist, html=True), name='frontend')
