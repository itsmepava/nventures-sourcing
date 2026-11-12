# Move nVentures from Windows to Linux

Before erasing Windows, close the app and back up its entire working folder to a private external drive. Include the hidden .env file, every SQLite .db file, OAuth client JSON stored outside the project, and any custom files. Stopping the app first avoids copying an inconsistent active SQLite database. Keep the original backup until the app and records have been verified on Linux. The release ZIP intentionally contains no credentials or real database.

Copy the project and its saved data to a folder owned by your Linux user. Do not reuse the Windows .venv or frontend/node_modules folders; dependencies are specific to the operating system. Recreate .venv on Linux. If a Windows .venv is present, rename it to .venv-windows before setup. A clean extract of this release plus your .env and database files is another option.

Install Python 3.11 or later and its venv support through your distribution's package manager. On Ubuntu/Debian, the usual packages are python3, python3-venv and python3-pip. From the project folder:

```sh
sh setup.sh
sh start.sh
```

The compiled React interface is bundled, so Node.js is not required for normal use. Setup installs Python dependencies from requirements.txt. If you use service-account outbound Sheets sync, also run `.venv/bin/python -m pip install -r backend/requirements-sheets.txt`. PostgreSQL remains optional.

The app opens http://127.0.0.1:8000. If the browser does not open automatically, visit that address yourself. Keep the terminal open; Ctrl+C stops the app.

Review .env for Windows-specific paths. A DATABASE_URL pointing at C:/Users/... must be changed to the actual Linux database location. SQLite absolute URLs have four slashes, for example `sqlite:////home/yourname/nventures/nventures.db`. Choose the correct saved database for APP_MODE. Back up first; do not point live mode at the demo database. File names on Linux are case sensitive.

Google credentials in .env may continue to work if still authorized. If reconnection is required, run `sh connect-google.sh` and authorize the expected account in your browser. Check that account can edit the inbound sheet. Do not paste secrets into chat. Protect .env and the downloaded OAuth client JSON with user-only permissions; setup applies chmod 600 to .env.

After migration, check existing company counts, run history and pitch records before starting a live run. Test the demo first. Retain your Windows backup until live connection checks succeed. The Linux scripts have been prepared on Windows; an actual Linux launch still needs verification on your chosen distribution.
