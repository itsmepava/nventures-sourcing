import copy
import io
import json
import os
import tempfile
import time
import unittest
import atexit
from unittest.mock import patch
from urllib.error import HTTPError

testdir = tempfile.TemporaryDirectory()
os.environ['APP_MODE'] = 'demo'
os.environ['DATABASE_URL'] = 'sqlite:///' + os.path.join(testdir.name, 'test.db')
from app import demo, integrations, main
from app.rules import qualifies, duplicate, name_key, domain_key, linkedin_key, COUNTRIES
from app.db import Base, engine, Session, Company, Partner, Run, Event, serialize
from app.engine import operation_lock, execute
from sqlalchemy import select

def cleanup():
    engine.dispose()
    import gc
    gc.collect()
    testdir.cleanup()
atexit.register(cleanup)

class RulesTests(unittest.TestCase):
    def setUp(self):
        self.record = demo.candidate(0, 'Test partner')

    def test_boundary_and_all_countries(self):
        for country in COUNTRIES:
            r = copy.deepcopy(self.record)
            r['hq_country'] = country
            r['evidence']['headquarters'][0]['quote'] = f'Test company is headquartered in {country}.'
            r['total_funding_usd'] = 3000000
            r['evidence']['funding'][0]['quote'] = 'Test company has raised a total of USD 3000000.'
            self.assertEqual(qualifies(r), [], country)

    def test_unknown_outside_hq_and_founder_nationality_fail(self):
        for value in ['', 'United States', 'United Kingdom', 'Unknown']:
            self.assertTrue(qualifies(self.record | {'hq_country': value}))
        self.assertTrue(qualifies(self.record | {'headquarters': ''}))
        r = copy.deepcopy(self.record)
        r['hq_country'] = 'India'
        r['evidence']['headquarters'][0]['quote'] = 'Headquartered in San Francisco. Its founders are from India.'
        self.assertTrue(qualifies(r))
        r['evidence']['headquarters'][0]['quote'] = 'Headquartered in San Francisco with operations in India.'
        self.assertTrue(qualifies(r))

    def test_funding_stage_and_types_fail_closed(self):
        for value in [3000001, None, -1, True, '1000000', float('nan'), float('inf')]:
            self.assertTrue(qualifies(self.record | {'total_funding_usd': value}), repr(value))
        for stage in ['series a', 'series b', 'growth', 'unknown', '']:
            self.assertTrue(qualifies(self.record | {'stage': stage}), stage)
        for b2b in [False, 'true', None]:
            self.assertTrue(qualifies(self.record | {'b2b': b2b}))
        self.assertTrue(qualifies(self.record | {'active': False}))

    def test_independent_maturity_and_funding_veto(self):
        r = copy.deepcopy(self.record)
        r['evidence']['stage'][0]['quote'] = 'Company closed its Series A round in 2025.'
        self.assertTrue(qualifies(r))
        r = copy.deepcopy(self.record)
        r['evidence']['funding'][0]['quote'] = 'Company raised a total of USD 21 million.'
        self.assertTrue(qualifies(r))
        self.assertTrue(qualifies(self.record | {'funding_claims_usd': [21000000]}))
        self.assertTrue(qualifies(self.record | {'evidence': {}}))
        r = copy.deepcopy(self.record)
        r['evidence']['funding'][0]['quote'] = 'Funding has not been disclosed publicly.'
        self.assertTrue(qualifies(r))
        r['evidence']['funding'][0]['quote'] = 'Company raised USD 800000 in its latest seed round.'
        self.assertTrue(qualifies(r))
        r['evidence']['funding'][0]['quote'] = 'Company raised a total of S$800000.'
        self.assertTrue(qualifies(r))

    def test_duplicate_normalization(self):
        indexes = [{'name_key': name_key('Acme Private Limited'), 'domain': domain_key('https://www.acme.com/path?x=1'), 'linkedin_key': linkedin_key('https://www.linkedin.com/company/acme/')}]
        self.assertTrue(duplicate({'name': 'ACME, Inc.'}, indexes))
        self.assertTrue(duplicate({'name': 'Different', 'website': 'acme.com'}, indexes))
        self.assertTrue(duplicate({'name': 'Different', 'linkedin': 'https://linkedin.com/company/ACME?x=1'}, indexes))
        self.assertFalse(duplicate({'name': 'New'}, indexes))

class IntegrationTests(unittest.TestCase):
    def test_retry_429_and_do_not_retry_401(self):
        response = io.StringIO('{"ok":true}')
        failure = HTTPError('url', 429, 'limit', {'Retry-After': '2'}, None)
        sleep = []
        with patch('app.integrations.urlopen', side_effect=[failure, response]) as network:
            self.assertTrue(integrations.request_json('https://example.com', sleep=sleep.append)['ok'])
            self.assertEqual(network.call_count, 2)
            self.assertGreaterEqual(sleep[0], 2)
        with patch('app.integrations.urlopen', side_effect=HTTPError('url', 401, 'bad', {}, None)) as network:
            with self.assertRaises(integrations.ProviderError):
                integrations.request_json('https://example.com', sleep=lambda _: None)
            self.assertEqual(network.call_count, 1)

    def test_model_fallback_and_bad_json(self):
        with patch.dict(os.environ, {'OPENROUTER_API_KEY': 'fake', 'OPENROUTER_MODELS': 'one,two'}), patch('app.integrations.request_json', side_effect=[integrations.ProviderError('limit'), {'choices': [{'message': {'content': '```json\n{"ok":true}\n```'}}]}]) as network:
            self.assertTrue(integrations.llm('test')['ok'])
            self.assertEqual(network.call_count, 2)
        with self.assertRaises(integrations.ProviderError):
            integrations.parse_json('broken response')

    def test_hallucinated_evidence_removed(self):
        r = demo.candidate(0, 'test')
        sources = [{'url': r['website'], 'content': 'A true headquarters excerpt.'}]
        with patch('app.integrations.llm', return_value=r):
            result = integrations.extract('LedgerLeaf', sources)
        self.assertTrue(all(not x for x in result['evidence'].values()))
        self.assertTrue(qualifies(result))

    def test_dynamic_38_column_alias_mapping(self):
        headers = ['Company Name', 'Product Descriptoon', 'Unique Value Propsotions', 'Founder Name ', 'Extra Notes', 'Extra Notes'] + ['Unknown'] * 32
        r = demo.candidate(0, 'test') | {'uvp': 'A useful value prop'}
        row = integrations.sheet_row(headers, r, 'Test partner')
        self.assertEqual(len(row), 38)
        self.assertEqual(row[:4], [r['name'], r['product'], r['uvp'], r['founders']])
        self.assertTrue(row[4] and row[5])

    def test_sheets_full_row_verification_and_retry_reconciliation(self):
        class FakeWorksheet:
            title = 'Active Sourcing'
            def __init__(self):
                self.rows = [['Company Name', 'Website', 'Country', 'Sector']]
                self.appends = 0
            def get_all_values(self): return self.rows
            def append_row(self, row, **kwargs):
                self.appends += 1
                self.rows.append(row)
                assert kwargs['value_input_option'] == 'RAW'
                return {'updates': {'updatedRange': "'Active Sourcing'!A2:D2"}}
            def row_values(self, n): return self.rows[n - 1]
        ws = FakeWorksheet()
        sheets = object.__new__(integrations.Sheets)
        sheets.ws = ws
        c = Company(data=demo.candidate(0, 'test'), partner='test')
        number, _, status = sheets.sync(c)
        self.assertEqual((number, status), (2, 'verified'))
        self.assertEqual(sheets.sync(c)[2], 'verified')
        self.assertEqual(ws.appends, 1)
        ws.rows[1][3] = 'Edited sector'
        self.assertEqual(sheets.sync(c)[2], 'already_exists')
        ws.rows = [ws.rows[0]]
        ws.row_values = lambda n: ['Wrong company']
        with self.assertRaises(integrations.ProviderError):
            sheets.sync(c)

class PipelineTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        from alembic.config import Config
        from alembic import command
        from app.config import ROOT
        command.upgrade(Config(str(ROOT / 'backend' / 'alembic.ini')), 'head')

    def setUp(self):
        with Session() as db:
            for table in [Event, Run, Company, Partner]:
                db.query(table).delete()
            db.add_all(Partner(name=n, website=w) for n, w in demo.PARTNERS)
            db.commit()

    def execute(self, target=25, cancelled=False):
        with Session() as db:
            r = Run(target=target, max_partners=6, mode='demo', cancel_requested=int(cancelled))
            db.add(r); db.commit(); run_id = r.id
        operation_lock.acquire()
        with patch('app.engine.time.sleep'), patch('app.integrations.request_json', side_effect=AssertionError('Demo must not access network')):
            execute(run_id)
        with Session() as db:
            return serialize(db.get(Run, run_id))

    def test_demo_pipeline_rejects_duplicates_and_partial_target(self):
        result = self.execute()
        self.assertEqual(result['status'], 'partial')
        self.assertEqual(result['accepted'], 12)
        self.assertEqual(result['rejected'], 1)
        self.assertEqual(result['duplicates'], 1)
        with Session() as db:
            self.assertEqual(len(db.scalars(select(Company)).all()), 12)
            self.assertTrue(all(not qualifies(c.data) for c in db.scalars(select(Company)).all()))
            self.assertTrue(all(p.run_count == 1 for p in db.scalars(select(Partner)).all()))
        result = self.execute()
        self.assertEqual(result['accepted'], 0)
        self.assertGreaterEqual(result['duplicates'], 12)

    def test_target_and_cancellation(self):
        self.assertEqual(self.execute(target=1)['status'], 'completed')
        self.assertEqual(self.execute(cancelled=True)['status'], 'cancelled')

    def test_concurrent_start_and_demo_sync_blocked(self):
        from fastapi import HTTPException
        operation_lock.acquire()
        try:
            with self.assertRaises(HTTPException) as e:
                main.start_run(main.RunInput())
            self.assertEqual(e.exception.status_code, 409)
        finally:
            operation_lock.release()
        with self.assertRaises(HTTPException):
            main.sync()
        with self.assertRaises(HTTPException):
            main.start_run(main.RunInput(sync_to_sheets=True))
        with patch('app.integrations.request_json', side_effect=AssertionError('Demo must not access network')):
            with self.assertRaises(HTTPException):
                main.check_integrations()

    def test_unique_constraints_and_export(self):
        self.execute(target=1)
        with Session() as db:
            c = db.scalar(select(Company))
            db.add(Company(name='Alias', name_key=c.name_key, data={}, partner='test'))
            from sqlalchemy.exc import IntegrityError
            with self.assertRaises(IntegrityError): db.commit()
        exported = main.export()
        self.assertIn(b'Company Name', exported.body)
        self.assertIn(b'LedgerLeaf', exported.body)

    def test_partner_failure_does_not_kill_remaining_run(self):
        from app import config
        with Session() as db:
            r = Run(target=1, max_partners=6, mode='live')
            db.add(r); db.commit(); rid = r.id
        candidate = demo.candidate(0, 'test') | {'demo': False}
        operation_lock.acquire()
        with patch.object(config, 'MODE', 'live'), patch.object(config, 'SYNC_ENABLED', False), patch.dict(os.environ, {'GOOGLE_SERVICE_ACCOUNT_JSON': '', 'SPREADSHEET_ID': ''}), patch('app.integrations.discover', side_effect=[integrations.ProviderError('Provider unavailable'), [{'name': candidate['name']}]]), patch('app.integrations.research', return_value=candidate):
            execute(rid)
        with Session() as db:
            r = db.get(Run, rid)
            self.assertEqual((r.status, r.accepted, r.errors), ('completed', 1, 1))

    def test_live_run_writes_each_company_and_tracks_verified_row(self):
        self.live_sync_run(False)

    def test_live_sync_error_keeps_company_and_is_reported(self):
        self.live_sync_run(True)

    def live_sync_run(self, fail):
        from app import config
        from unittest.mock import MagicMock
        candidate = demo.candidate(0, 'test') | {'demo': False}
        sheet = MagicMock()
        sheet.existing.return_value = []
        sheet.partners.return_value = []
        sheet.sync.side_effect = integrations.ProviderError('Read-back failed') if fail else None
        sheet.sync.return_value = (47, "'Active Sourcing'!A47:AL47", 'verified')
        with Session() as db:
            r = Run(target=1, max_partners=1, mode='live', sync_to_sheets=1)
            db.add(r); db.commit(); rid = r.id
        operation_lock.acquire()
        with patch.object(config, 'MODE', 'live'), patch.object(config, 'SYNC_ENABLED', True), patch('app.integrations.Sheets', return_value=sheet), patch('app.integrations.discover', return_value=[{'name': candidate['name']}]), patch('app.integrations.research', return_value=candidate):
            execute(rid)
        with Session() as db:
            r, c = db.get(Run, rid), db.scalar(select(Company))
            self.assertEqual(r.accepted, 1)
            self.assertEqual(r.synced, 0 if fail else 1)
            self.assertEqual(r.sync_errors, 1 if fail else 0)
            self.assertEqual(r.status, 'completed_with_errors' if fail else 'completed')
            self.assertEqual(c.sync_status, 'error' if fail else 'verified')
            self.assertEqual(c.sheet_row, None if fail else 47)
            self.assertTrue(db.scalar(select(Event).where(Event.kind == ('sync_error' if fail else 'sync'))))
        sheet.sync.assert_called_once()
        sheet.control.assert_called_once()

    def test_live_connections_read_tests_without_writes(self):
        from app import config
        from unittest.mock import MagicMock
        sheet = MagicMock()
        with patch.object(config, 'MODE', 'live'), patch('app.integrations.request_json', return_value={'data': {}}), patch('app.integrations.search', return_value=[]), patch('app.integrations.Sheets', return_value=sheet):
            result = main.check_integrations()
        self.assertTrue(all(x['status'] == 'connected' for x in result['results']))
        sheet.sync.assert_not_called()

    def test_database_transfer_preserves_rows_and_refuses_overwrite(self):
        from transfer_database import transfer
        from sqlalchemy import create_engine
        from alembic.config import Config
        from alembic import command
        from app.config import ROOT
        self.execute(target=1)
        target_url = 'sqlite:///' + os.path.join(testdir.name, 'target.db')
        target_engine = create_engine(target_url)
        # Test the frozen Alembic schema, rather than create_all from current models.
        with patch('app.db.engine', target_engine):
            command.upgrade(Config(str(ROOT / 'backend' / 'alembic.ini')), 'head')
        target_engine.dispose()
        counts = transfer(str(engine.url), target_url)
        self.assertEqual(counts['companies'], 1)
        self.assertEqual(counts['runs'], 1)
        self.assertGreater(counts['events'], 1)
        with self.assertRaises(ValueError): transfer(str(engine.url), target_url)

    def test_legacy_import_is_read_only_and_idempotent(self):
        import sqlite3
        from import_legacy_history import import_history
        path = os.path.join(testdir.name, 'legacy.db')
        with sqlite3.connect(path) as old:
            old.execute('CREATE TABLE IF NOT EXISTS runs (id INTEGER, started_at TEXT, finished_at TEXT, target INTEGER, accepted_count INTEGER)')
            old.execute("INSERT INTO runs VALUES (1, '2026-01-01T00:00:00Z', '2026-01-01T00:10:00Z', 25, 3)")
        old.close()
        with patch('builtins.print'):
            import_history(path)
            with Session() as db: self.assertEqual(len(db.scalars(select(Run)).all()), 0)
            import_history(path, apply=True)
            import_history(path, apply=True)
        with Session() as db:
            runs = db.scalars(select(Run)).all()
            self.assertEqual(len(runs), 1)
            self.assertEqual((runs[0].status, runs[0].accepted), ('imported', 3))
        with sqlite3.connect(path) as old:
            self.assertEqual(old.execute('SELECT COUNT(*) FROM runs').fetchone()[0], 1)
        old.close()

if __name__ == '__main__':
    unittest.main()
