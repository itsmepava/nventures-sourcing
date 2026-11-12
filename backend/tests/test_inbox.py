import base64
import os
import unittest
from unittest.mock import patch
from app.inbox import Google, ProviderError
from app.inbox import extract, body_text, HEADERS

class InboxTests(unittest.TestCase):
    def setUp(self):
        setting=patch.dict(os.environ, {'INBOX_SPREADSHEET_ID':'fixture-inbound-sheet'})
        setting.start()
        self.addCleanup(setting.stop)

    def test_missing_destination_fails_before_network_write(self):
        google=object.__new__(Google)
        with patch.dict(os.environ, {'INBOX_SPREADSHEET_ID':''}), patch.object(google,'call') as call:
            with self.assertRaises(ProviderError):
                google.rows()
            call.assert_not_called()

    def test_sheet_write_raw_and_full_readback(self):
        google=object.__new__(Google)
        data=dict(name='New Startup',description='Useful product',hq='',contact='a@example.com',notes='Source: message123')
        row=['New Startup','Useful product','','a@example.com','','Source: message123']
        with patch.object(google,'call',side_effect=[{'values':[HEADERS]}, {'updates':{'updatedRange':"'Sheet1'!A2:F2"}}, {'values':[row]}]) as call:
            self.assertEqual(google.sync(data),('verified',2))
            self.assertIn('valueInputOption=RAW',call.call_args_list[1].args[0])
            self.assertTrue(call.call_args_list[1].kwargs['write'])

    def test_existing_decision_never_overwritten(self):
        google=object.__new__(Google)
        with patch.object(google,'call',return_value={'values':[HEADERS,['New Startup','Old description','','','Passed','Manual notes']]}) as call:
            self.assertEqual(google.sync({'name':'New Startup'}),('already_exists',2))
            self.assertEqual(call.call_count,1)

    def test_global_startup_not_outbound_mandate(self):
        data = extract('Company name: Polar Labs\nWebsite: https://example.com\nCountry of incorporation: Canada\nStage: Series A\nOne line on what you do: Consumer software startup building a new way to share creative projects. Raising $9 million.\nContact email: founder@example.com')
        self.assertEqual(data['status'], 'ready')
        self.assertEqual(data['hq'], '')
        self.assertIn('Canada', data['notes'])
        self.assertEqual(data['name'], 'Polar Labs')

    def test_unstructured_and_large_company_need_review(self):
        self.assertEqual(extract('Please see our deck', 'a@example.com')['status'], 'review')
        self.assertEqual(extract('Company name: Big Co\nOne line on what you do: A publicly traded multinational operating across many countries.\nContact email: a@example.com')['status'], 'review')

    def test_each_message_independent(self):
        a=extract('Company name: Alpha\nOne line on what you do: A startup making tools for businesses to manage inventory.\nContact email: a@example.com')
        b=extract('Company name: Beta\nOne line on what you do: A startup making tools for businesses to manage inventory.\nContact email: b@example.com')
        self.assertNotEqual(a['name'],b['name'])

    def test_nested_mime(self):
        raw=base64.urlsafe_b64encode(b'Company name: Example').decode().rstrip('=')
        self.assertIn('Example',body_text({'parts':[{'mimeType':'text/plain','body':{'data':raw}}]}))
        self.assertEqual(len(HEADERS),6)

    def test_tests_skipped_and_incorporation_not_hq(self):
        p=extract('Company name: Test Company\nOne line on what you do: This is a test form for this application only.\nCountry of incorporation: 2025\nContact email: test@example.com')
        self.assertEqual(p['status'],'skipped')
        self.assertEqual(p['hq'],'')
