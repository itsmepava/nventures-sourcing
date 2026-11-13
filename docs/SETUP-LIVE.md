# Getting nVentures fully running (live Gmail + Google Sheets)

Every step, in order. Steps marked **(you)** need your own Google login or your own computer; nothing here costs money.

## Part A — Get the app onto your computer (Linux)

1. Open a terminal and go to your project folder (the folder containing `start-go.sh`).
2. Stop the app if it is running: press **Ctrl+C** in the terminal where it runs.
3. Download the latest code:
   ```
   git fetch origin
   git checkout claude/go-redesign-b8xpo1
   git pull
   ```
4. Delete the old built program so it is rebuilt with the new code:
   ```
   rm -f bin/nventures
   ```
5. If Go is not installed, install the local copy (no system changes):
   ```
   python3 scripts/bootstrap_go.py
   ```
6. Build the program and check everything works before touching real data:
   ```
   CGO_ENABLED=0 .tools/go/bin/go build -trimpath -o bin/nventures ./cmd/nventures
   ./bin/nventures rehearse
   ```
   (If Go is installed system-wide, use `go` instead of `.tools/go/bin/go`.) The second command must end with **PASS**.

Windows instead: download `nventures-0.5.0-alpha.1-windows-amd64.exe` from the release page, rename it `nventures.exe`, put it next to `Start nVentures (Go).cmd`, and double-click that file in step C9. (The Windows build has not been tested yet.)

## Part B — Prepare the Google Sheets (you)

7. **Inbound sheet** (pitches from Gmail). Use an existing sheet or create one. In the tab you will use, row 1 must be exactly these six headers, in this order, in columns A–F:

   `Company Name` · `Description` · `HQ` · `Contact` · `Status` · `Notes`

   Leave everything else as it is. The app only appends new rows, leaves **Status** blank and never changes existing cells.
8. **Outbound sheet** (sourced companies). Your existing legacy 38-column sheet works as-is. At minimum, its sourcing tab needs a `Company Name` header in row 1. Other recognised headers, including the old typos `Product Descriptoon` and `Unique Value Propsotions`, are filled in automatically, and unknown headers are left blank.
   Columns the app fills for a new row: the company facts it evidenced, `Business Model` (B2B), `Outreach status` (Not Reached Out), `Status` (Sourced), `IC Recommendation` (Potential Fit), `Reason`, `Extra Notes`, `source` and `Updates`/`Updates (sourcing sheet)`. Analyst columns (`Relevance Flag`, `Domain Knowledge`, `Track Record`, `Technical Knowledge`, `Sales Skills`), `Founder Emails`, `Pre-money Evaluation` and `Amount to be Raised` are left blank: nothing is invented. If the tab has no `Total Funding USD` column, the evidenced total is written into `Extra Notes`.
9. In the same outbound sheet, check these two tabs:
   - **Partner list tab** (default name `Partner List`): row 1 has `Company Name` (the partner fund's name) plus where to start crawling: `Company Portfolio` (a link to the fund's portfolio page, preferred) or `Website`. A cell that isn't a web address (e.g. "Yes") is ignored. If the cell is blank, the app tries to find the website itself, from the `Contact` email's domain and from the fund's name, and marks what it finds **found — confirm** on the Partners page. Optional: add `TAVILY_API_KEY=...` to `.env` and switch on **Settings → Providers & data → Search the web (Tavily) for missing partner websites** to search like the old app did (Tavily's free allowance, then paid). An optional `nVentures Relevance` column set to `No` excludes a partner. Other columns are ignored.
   - **Partner control tab** (default name `Sourcing Control`, optional): row 1 is exactly `Partner Name` · `Last Sourced UTC` · `Run Count` · `Last Run Added`. Rows below can be empty; the app fills them. Without this tab, partner rotation is still kept inside the app; the tab just makes it visible in the sheet.
10. Write down, for later:
    - the **inbound sheet ID** and the **outbound sheet ID**: the long code in each sheet's URL between `/d/` and `/edit`;
    - the exact **tab names** (inbound tab, outbound sourcing tab, partner list tab, partner control tab);
    - the **Gmail address** that receives pitches.
11. Make sure the Google account you will connect with can **edit** both sheets (Share → Editor).

## Part C — Create the Google connection (you, about 15 minutes)

12. Go to <https://console.cloud.google.com/> and sign in.
13. At the top, open the project picker → **New project** → name it `nVentures` → **Create**, and make sure it is selected.
14. **APIs & Services → Library**: search **Gmail API** → **Enable**. Go back, search **Google Sheets API** → **Enable**.
15. **APIs & Services → OAuth consent screen** (may be called *Google Auth Platform → Branding/Audience*):
    - **User type**: choose **Internal** if the pitch mailbox is a Google Workspace account in your organisation. Otherwise choose **External**.
    - App name `nVentures`, support email = your email, developer contact = your email → **Save**.
    - **Scopes / Data access → Add or remove scopes**: add `https://www.googleapis.com/auth/gmail.readonly` and `https://www.googleapis.com/auth/spreadsheets` → **Save**.
    - If you chose **External**: under **Test users / Audience**, add the pitch mailbox address (and your own if different).
16. **Important for External apps:** while the app's publishing status is **Testing**, Google cancels the connection after **7 days** and you must click *Reconnect Google* again. To avoid this, on the *Audience* page click **Publish app** (“In production”). Google will show an “unverified app” warning when you connect; that is expected for a private tool. Click **Advanced → Go to nVentures (unsafe)** and continue. Internal apps do not have this limit.
17. **APIs & Services → Credentials → Create credentials → OAuth client ID**:
    - Application type: **Desktop app**; name `nVentures desktop`.
    - **Create**, then **Download JSON**. Keep this file private: don't email it, commit it or paste it into chat.

## Part D — Configure and connect the app

18. In the project folder, create or edit the file `.env`, adding these lines (keep any lines already there):
    ```
    APP_MODE=live
    ```
    Optionally you can also set the destinations here instead of in step 23:
    ```
    GMAIL_EXPECTED_ACCOUNT=pitches@yourdomain.com
    INBOX_SPREADSHEET_ID=<inbound sheet ID>
    INBOX_SHEET_TAB=<inbound tab name>
    SPREADSHEET_ID=<outbound sheet ID>
    SOURCING_TAB_NAME=<outbound sourcing tab name>
    PARTNER_TAB_NAME=<partner list tab name>
    CONTROL_TAB_NAME=<partner control tab name>
    ```
19. Start the app:
    ```
    ./start-go.sh
    ```
    Your browser opens <http://127.0.0.1:8765>. Live data is kept separate from the demo data you had before.
20. **Set up your workspace**: enter your name, email and a password of at least 10 characters. This creates the admin account.
21. Go to **Settings → Google connection**. Under **OAuth client JSON file**, choose the file from step 17 → **Store client file**. It shows *Client stored locally*.
22. Click **Connect Google**. Sign in as the **pitch mailbox** and approve both permissions: read Gmail, and edit Sheets. You return to the app with *Google connected.*
    - If the signed-in account differs from the expected mailbox, nothing is saved. Connect again with the right account.
23. (If you didn't use step 18's optional lines.) Go to **Settings → Destinations** and fill in:
    - **Expected Gmail account**: the pitch mailbox;
    - **Inbound sheet ID** and **Inbound tab**;
    - **Outbound sheet ID**, **Outbound tab**, **Partner list tab**, **Partner control tab**.

    Leave **Allow writing sourced companies to the outbound sheet** off for now, then click **Save destinations**.
24. Click **Test connection (read-only)**. Every line should be green: mailbox matches, inbound headers OK, outbound sheet readable. Nothing is written. If anything is red, fix what it says (usually a header or tab name) and test again.

## Part E — First live run, carefully

25. **Pitch inbox → Import from Gmail**: set **Messages to process this run** to `10`, leave **Then sync approved pitches** unticked → **Start import**.
26. When it finishes, open a few pitches. Check the name, description, HQ and contact; fix what's wrong.
27. For a pitch with a PDF/PPTX attachment, click **Read deck text**. Check the facts, then use **Use** or **Add to notes** if helpful.
28. Approve 2–3 good pitches with **Approve**, then select them and click **Sync selected now** (or use **Approve & sync now** in a pitch).
29. Open the inbound sheet and confirm the new rows, with Status blank. The pitch's **row link** in the app should open the exact row.
30. **Sourcing → Start sourcing run**: target `3`, leave **Write each accepted company to the outbound sheet** unticked → **Start run**. Review the accepted companies and their evidence quotes.
31. If the results look right: **Settings → Destinations →** tick **Allow writing sourced companies to the outbound sheet** → **Save destinations**, then on **Sourcing** click **Sync pending companies**. Check the outbound sheet.
32. From now on, imports can run with sync ticked, and larger limits are fine. Re-running never creates duplicate rows.

## Part F — Team and daily use (optional)

33. **Team → Add a member**: name, email, role (*Sourcer*, *Pitch responder*, *Viewer* or *Admin*) → **Add member**. Give them the one-time password privately; they choose their own at first sign-in. People use the app on this computer (it is deliberately not reachable from other machines).
34. **Settings → Automation** (admins): add schedules, for example an inbox import every 4 hours with sync, and a weekly sourcing run. Schedules run only while the app is running.
35. Backups: from the project folder, `./bin/nventures backup --env .env` writes a consistent copy of the database into the data folder's `backups/`. Do this before upgrades.

## If something goes wrong

| Problem | Fix |
|---|---|
| “another nVentures process is already using this data directory” | The app is already running; open <http://127.0.0.1:8765> or stop the other one |
| Google says “access blocked” / “app not verified” with no way through | External app in Testing without the mailbox added as a test user (step 15), or wrong account |
| Connected but imports fail with a 401/403 | Connection expired (step 16) or scopes not approved: **Settings → Reconnect Google** |
| Test shows inbound headers wrong | Row 1 must be exactly the six headers in step 7, columns A–F |
| *Write uncertain* on a row | Network dropped mid-write; click sync again — the app checks the sheet first and never duplicates |
| Anything else | Copy the exact message from the screen (or `nventures.log` in the data folder; it contains no secrets) and send it to the developer/Claude |

To go back to demo mode at any time, set `APP_MODE=demo` in `.env` and restart; live data is untouched.
