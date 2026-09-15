# nVentures Sourcing Web App

First production-oriented version of the nVentures AI sourcing tool.

## Features
- Team login
- Admin/user roles
- Dashboard
- Run history
- OpenRouter + Tavily
- Existing Google Sheet as output
- Hard South Asia + Singapore headquarters filter
- Existing notebook sourcing engine preserved as the core
- Google Sheets write/read-back verification inherited from the notebook engine

## 1. Install

```bash
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

Windows PowerShell:
```powershell
.venv\Scripts\Activate.ps1
pip install -r requirements.txt
```

## 2. Google service account

Create a Google Cloud service account, enable Google Sheets/Drive APIs, download its JSON key, and share the target Google Sheet with the service account email as Editor.

Set `GOOGLE_SERVICE_ACCOUNT_JSON` to the JSON contents (or base64-encoded JSON).

## 3. Secrets

Copy `.env.example` to `.env` and populate:
- OPENROUTER_API_KEY
- TAVILY_API_KEY
- GOOGLE_SERVICE_ACCOUNT_JSON
- ADMIN_EMAIL
- ADMIN_PASSWORD

For hosted deployment, put these in the platform's secret/environment-variable manager instead of committing `.env`.

## 4. Run

```bash
streamlit run app.py
```

## Important security note

The original Colab contained API keys. Treat those keys as exposed and rotate them before using this app. The app is designed to use replacement keys from environment variables.

## Important architecture note

The app wraps the proven Cell 9 sourcing engine from `nventure(2).ipynb` instead of rewriting the research logic. This minimizes behavioral changes while moving from Colab to a web interface.

Before production use, run several small test runs and verify:
1. accepted companies appear in Active Sourcing,
2. San Francisco-headquartered companies are rejected,
3. Singapore-headquartered companies can pass geography,
4. duplicates are skipped,
5. run history is recorded.
