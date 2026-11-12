import os
import threading
import time
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from . import config, demo, integrations
from .db import Session, Company, Partner, Run, audit, now, serialize
from .rules import qualifies, duplicate, name_key, domain_key, linkedin_key

# One local worker prevents concurrent Sheets writes and partner reservations.
operation_lock = threading.Lock()

def safe_error(exc):
    # Never persist provider response bodies, keys, prompts or credential dumps.
    return str(exc) if isinstance(exc, integrations.ProviderError) else f'{type(exc).__name__}: operation failed; check configuration or retry'

def sync_company(db, sheets, company, run_id=None):
    reasons = qualifies(company.data)
    if company.data.get('demo') or reasons:
        company.sync_status = 'blocked'
        company.sync_error = 'Demo company cannot sync' if company.data.get('demo') else '; '.join(reasons)
        db.commit()
        return 'blocked'
    try:
        company.sheet_row, company.sheet_range, company.sync_status = sheets.sync(company)
        company.sync_error = None
        audit(db, 'sync', f'{company.name}: {company.sync_status}' + (f' · verified sheet row {company.sheet_row}' if company.sheet_row else ''), run_id, {'company_id': company.id, 'row': company.sheet_row, 'range': company.sheet_range})
    except Exception as exc:
        company.sync_status, company.sync_error = 'error', safe_error(exc)
        audit(db, 'sync_error', f'{company.name}: {company.sync_error}', run_id, {'company_id': company.id})
    return company.sync_status

def execute(run_id):
    try:
        with Session() as db:
            run = db.get(Run, run_id)
            run.status = 'running'
            db.commit()
            sheets = integrations.Sheets() if config.MODE == 'live' and (config.SYNC_ENABLED or (os.getenv('GOOGLE_SERVICE_ACCOUNT_JSON') and os.getenv('SPREADSHEET_ID'))) else None
            if run.sync_to_sheets and (config.MODE != 'live' or not config.SYNC_ENABLED or sheets is None):
                raise integrations.ProviderError('Immediate sheet writing requires live mode and configured Sheets sync')
            remote_indexes = []
            if sheets:
                remote_indexes = sheets.existing()
                for incoming in sheets.partners():
                    p = db.scalar(select(Partner).where(Partner.name == incoming['name']))
                    if p is None:
                        p = Partner(**incoming)
                        db.add(p)
                    else:
                        p.website, p.relevant = incoming['website'], incoming['relevant']
                        p.last_sourced = max(p.last_sourced, incoming['last_sourced'])
                        p.run_count = max(p.run_count, incoming.get('run_count', 0))
                db.commit()
            partners = db.scalars(select(Partner).where(Partner.relevant == 1).order_by(Partner.last_sourced, Partner.run_count, Partner.id).limit(run.max_partners)).all()
            if not partners:
                raise integrations.ProviderError('No relevant partners. Add a partner or enable Sheets integration.')
            for partner_index, partner in enumerate(partners):
                db.refresh(run)
                if run.cancel_requested or run.accepted >= run.target or run.researched >= config.MAX_RESEARCH:
                    break
                added = 0
                audit(db, 'partner', f'Researching {partner.name}', run.id)
                try:
                    if config.MODE == 'demo':
                        fixture_index = next((i for i, (n, _) in enumerate(demo.PARTNERS) if n == partner.name), partner_index)
                        candidates = demo.candidates(fixture_index, partner.name)
                    else:
                        candidates = integrations.discover(partner)
                    for candidate in candidates:
                        db.refresh(run)
                        if run.cancel_requested or run.accepted >= run.target or run.researched >= config.MAX_RESEARCH:
                            break
                        run.message = f'Researching {candidate["name"]} · {partner.name}'
                        run.progress = min(95, int(run.researched / config.MAX_RESEARCH * 100))
                        db.commit()
                        try:
                            # Skip known names before spending on deep research.
                            local = [serialize(c) for c in db.scalars(select(Company)).all()]
                            if duplicate(candidate, local + remote_indexes):
                                run.duplicates += 1
                                audit(db, 'duplicate', f'{candidate["name"]}: already sourced', run.id)
                                continue
                            if config.MODE == 'demo':
                                time.sleep(.5)
                                record = candidate
                            else:
                                record = integrations.research(candidate['name'])
                            run.researched += 1
                            reasons = qualifies(record)
                            if reasons:
                                run.rejected += 1
                                audit(db, 'rejected', f'{record["name"]}: ' + '; '.join(reasons), run.id, {'reasons': reasons, 'name': record['name']})
                            elif duplicate(record, local + remote_indexes):
                                run.duplicates += 1
                                audit(db, 'duplicate', f'{record["name"]}: matching website or LinkedIn', run.id)
                            else:
                                company = Company(name=record['name'], name_key=name_key(record['name']), domain=domain_key(record.get('website', '')), linkedin_key=linkedin_key(record.get('linkedin', '')), data=record, partner=partner.name, sync_status='demo' if config.MODE == 'demo' else 'pending')
                                db.add(company)
                                try:
                                    db.commit()
                                except IntegrityError:
                                    db.rollback()
                                    run = db.get(Run, run_id)
                                    run.duplicates += 1
                                    audit(db, 'duplicate', f'{record["name"]}: unique constraint prevented duplicate', run.id)
                                    continue
                                run.accepted += 1
                                added += 1
                                audit(db, 'accepted', f'{company.name}: accepted and saved locally', run.id, {'company_id': company.id})
                                if run.sync_to_sheets:
                                    run.message = f'Writing and verifying {company.name} in Google Sheets'
                                    db.commit()
                                    status = sync_company(db, sheets, company, run.id)
                                    if status == 'verified':
                                        run.synced += 1
                                    elif status in ('error', 'blocked'):
                                        run.sync_errors += 1
                                        run.errors += 1
                                    db.commit()
                        except Exception as exc:
                            db.rollback()
                            run = db.get(Run, run_id)
                            run.errors += 1
                            audit(db, 'error', f'{candidate.get("name", "Candidate")}: {safe_error(exc)}', run.id)
                        db.commit()
                except Exception as exc:
                    db.rollback()
                    run = db.get(Run, run_id)
                    run.errors += 1
                    audit(db, 'error', f'{partner.name}: {safe_error(exc)}', run.id)
                finally:
                    partner.last_sourced, partner.last_added = now(), added
                    partner.run_count += 1
                    db.commit()
                    if sheets and config.SYNC_ENABLED and run.sync_to_sheets:
                        try:
                            sheets.control(partner)
                        except Exception as exc:
                            run.errors += 1
                            audit(db, 'error', 'Partner control sync: ' + safe_error(exc), run.id)
            db.refresh(run)
            run.status = 'cancelled' if run.cancel_requested else 'completed' if run.accepted >= run.target else 'partial'
            if run.status == 'completed' and run.sync_errors:
                run.status = 'completed_with_errors'
            run.finished_at, run.progress = now(), 100
            run.message = f'{run.accepted}/{run.target} accepted · {run.rejected} rejected · {run.duplicates} duplicates · {run.errors} errors'
            if run.sync_to_sheets:
                run.message += f' · {run.synced} verified sheet writes · {run.sync_errors} sync failures'
            audit(db, 'finished', run.message, run.id)
    except Exception as exc:
        with Session() as db:
            run = db.get(Run, run_id)
            run.status, run.finished_at, run.message = 'failed', now(), safe_error(exc)
            audit(db, 'error', run.message, run.id)
    finally:
        operation_lock.release()

