# Everyday workspace features

The theme controls above every page offer Light, Dark, and System. System follows your computer's appearance and updates when it changes. Theme is applied before the interface loads to avoid a bright flash. Compact view reduces table, card and navigation spacing. Reduced-motion settings from your computer are respected.

Press Ctrl+K on Windows or Cmd+K on macOS to open quick actions. You can also click Quick actions, or press ? outside an input. Search for a page or Refresh workspace. Tab moves between results; Enter selects; Escape closes. Dialogs keep keyboard focus inside and provide visible focus indicators. A Skip to content link is available to keyboard users.

Company database supports saved search/country/stage filters, Clear filters, newest/name/funding sorting, page sizes of 10/25/50/100, and filtered CSV export. Star companies for a personal shortlist and use Starred only to focus on it. Starred records are scoped by demo/live identity. The original Export CSV action exports the whole database; Export these results exports the current filters and shortlist across all pages. Formula-like values in CSV exports are escaped for spreadsheet safety.

Pitch inbox supports search by company, description or contact; status filters; 24-card pagination; filtered CSV export; and a choice of 25/100/250/500 pending messages per run. Synced pitch details are read-only. Existing investment decisions in Google Sheets remain untouched. Inbox polling pauses while the browser tab is hidden and resumes on the next visible polling cycle.

The app remembers your last page, appearance, company filters, table settings, pitch search/filter and import size in this browser on this computer. These are personal preferences, not shared investment decisions, and can be cleared using the browser's site-data controls. Private/incognito windows or blocked browser storage may not retain preferences. Live/demo safety is deliberately not remembered: the inbox opens in safe demo mode after reload.

Connection failures display actionable messages. An offline notice distinguishes loss of internet from access to the local app. A recovery screen offers reload if the interface crashes; saved records remain in the database. This app still requires its local Python server to be running. It does not cache Gmail or your database for use when that server is stopped.

Start.cmd remains the single launcher. No new third-party dependencies were added for these features. Google connection is still a one-time setup documented in INBOX.md. Attachments and external decks require manual review. This release is localhost-only, with no multi-user login or hosted deployment.
