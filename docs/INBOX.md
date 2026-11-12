# Inbound pitch funnel

Open Start.cmd, then Pitch inbox. Safe demo uses fictional records and never contacts Google. Outbound sourcing keeps its own mandate and configuration.

## Connect once

1. In Google Cloud, create/select a project, enable Gmail API and Google Sheets API, configure OAuth consent, and add contact@nventures.sg as a test user if the consent app is in testing. A Workspace internal app may be appropriate for your organization. Testing-mode grants may expire and require reconnection.
2. Create an OAuth client of type **Desktop app** and download its client JSON. Keep this file private, outside the project.
3. Double-click **Connect Google.cmd**, supply that JSON's path, and authorize **contact@nventures.sg** in the system browser. The requested permissions are Gmail read-only and Google Sheets editing. No Gmail send or modify permission is requested.
4. Restart Start.cmd. In Pitch inbox, disable Safe demo, then click **Import new pitches**. The mailbox is checked before reading messages. Google credentials are stored in your local .env, never in the frontend or release archive. Protect that file as you would an API key. The ChatGPT Gmail connection cannot be reused by this standalone app.

Set INBOX_SPREADSHEET_ID to your separate inbound sheet and INBOX_SHEET_TAB to its tab (default Sheet1), with headers Company Name, Description, HQ, Contact, Status, Notes. Override INBOX_SPREADSHEET_ID and INBOX_SHEET_TAB in .env if needed. The selected Google account must have editor access. Status remains blank for new records; existing rows and investment decisions are never overwritten.

The OAuth flow follows Google's Desktop application guidance: https://developers.google.com/identity/protocols/oauth2/native-app

## What the button does

Searches Gmail by GMAIL_PITCH_QUERY (configurable in .env), reads each message independently including messages in the same thread, extracts structured nVentures website submissions locally, and saves a review queue. Detailed structured submissions are ready; incomplete, unstructured or obvious established-business signals require review. This is a practical intake screen, not a promise of investment quality or comprehensive company-size verification. Review the queue periodically.

No geography, B2B, funding amount or round limit is applied to inbound. HQ remains unconfirmed unless a reviewer fills it; incorporation is retained in Notes. Direct emails need review and editing. HTML-only messages, attachments and linked decks are not automatically extracted in this release. No email content is sent to OpenRouter. Open the original email to review these materials.

Approve edited pitches or skip them in the detail view. On the next import, ready pitches sync to the sheet. Each write uses RAW values, records the actual sheet row and reads back all six cells. Existing normalized names or matching website references are treated as duplicates. Ambiguous write failures are retained; retry reconciles against the sheet before appending. Name changes or aliases may still need manual review.

The UI imports up to 100 pending messages per run; the API supports up to 500. Scans are bounded to 20 Gmail pages. History warns when more messages may exist. Repeated clicks progress past previously processed messages. This is button-triggered automation, not a background scheduled service. Stop takes effect after the current message/request.

Optional .env settings:

```
GMAIL_EXPECTED_ACCOUNT=contact@nventures.sg
INBOX_SPREADSHEET_ID=YOUR_INBOUND_SPREADSHEET_ID
INBOX_SHEET_TAB=Sheet1
GMAIL_PITCH_QUERY=-in:spam -in:trash -in:sent {"Company name:" "pitch deck" subject:fundraising subject:pitch}
```

All messages matching your query may contain personal data. Local records and .env remain on this computer. The application does not reply, archive, label or mark mail read. Back up the SQLite database before moving installations. The new version applies a migration that adds two inbound tables and retains outbound data.
