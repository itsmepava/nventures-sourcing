import base64
import json
import os

import gspread
from google.auth.exceptions import RefreshError
from google.oauth2.service_account import Credentials

SCOPES = [
    "https://www.googleapis.com/auth/spreadsheets",
    "https://www.googleapis.com/auth/drive",
]

REQUIRED_KEYS = ("type", "project_id", "private_key", "client_email", "token_uri")


def load_service_account_info():
    """Parse GOOGLE_SERVICE_ACCOUNT_JSON and fail with a specific message."""
    raw = os.getenv("GOOGLE_SERVICE_ACCOUNT_JSON", "").strip()

    if not raw:
        raise RuntimeError(
            "GOOGLE_SERVICE_ACCOUNT_JSON is missing. Create a Google service "
            "account, share the spreadsheet with it, and put the "
            "service-account JSON in this environment variable / Streamlit secret."
        )

    data = None
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        try:
            data = json.loads(base64.b64decode(raw).decode("utf-8"))
        except Exception as exc:  # noqa: BLE001
            raise RuntimeError(
                "GOOGLE_SERVICE_ACCOUNT_JSON is not valid JSON and is not valid "
                "base64-encoded JSON. Paste the service-account key file "
                "contents exactly as downloaded."
            ) from exc

    if not isinstance(data, dict):
        raise RuntimeError(
            "GOOGLE_SERVICE_ACCOUNT_JSON did not parse to a JSON object."
        )

    missing = [key for key in REQUIRED_KEYS if not data.get(key)]
    if missing:
        raise RuntimeError(
            f"Service-account JSON is missing required field(s): "
            f"{', '.join(missing)}. This usually means an OAuth client secret "
            f"was pasted instead of a service-account key. The file should "
            f"have \"type\": \"service_account\"."
        )

    if data.get("type") != "service_account":
        raise RuntimeError(
            f"Expected a service-account key but got type "
            f"'{data.get('type')}'. Download a key from IAM & Admin > "
            f"Service Accounts > Keys."
        )

    # Streamlit secrets and some env-var editors turn real newlines in the
    # private key into the two characters backslash-n. Restore them.
    private_key = data["private_key"]
    if "\\n" in private_key and "\n" not in private_key:
        data["private_key"] = private_key.replace("\\n", "\n")

    if "BEGIN PRIVATE KEY" not in data["private_key"]:
        raise RuntimeError(
            "The private_key field does not look like a PEM key. It was "
            "probably truncated or reformatted when it was pasted."
        )

    return data


def describe_credentials():
    """Non-secret identifying fields, for showing in the UI when auth fails."""
    try:
        data = load_service_account_info()
    except RuntimeError as exc:
        return {"error": str(exc)}

    return {
        "client_email": data.get("client_email", ""),
        "project_id": data.get("project_id", ""),
        "private_key_id": data.get("private_key_id", "")[:8] + "...",
    }


def get_client():
    data = load_service_account_info()
    creds = Credentials.from_service_account_info(data, scopes=SCOPES)

    try:
        return gspread.authorize(creds)
    except RefreshError as exc:
        raise RuntimeError(_refresh_error_help(exc, data)) from exc


def _refresh_error_help(exc, data):
    detail = str(exc)
    client_email = data.get("client_email", "unknown")
    project_id = data.get("project_id", "unknown")

    if "account not found" in detail:
        cause = (
            "Google does not recognise this service account. The account was "
            "most likely deleted, or its Google Cloud project was deleted or "
            "shut down. A key file for a deleted account keeps working "
            "locally and fails only when Google is asked for a token, which "
            "is exactly what happened here.\n\n"
            "Fix: create a new service account, download a fresh JSON key, "
            "replace GOOGLE_SERVICE_ACCOUNT_JSON, and share the spreadsheet "
            "with the new client_email as an Editor."
        )
    elif "Invalid JWT Signature" in detail:
        cause = (
            "The service account exists but this key is no longer valid. The "
            "key was probably deleted or rotated. Download a fresh key and "
            "replace GOOGLE_SERVICE_ACCOUNT_JSON."
        )
    elif "JWT" in detail and ("early" in detail or "expired" in detail):
        cause = (
            "The signed token was rejected on timing grounds, which points at "
            "a clock-skew problem on the host rather than the key itself."
        )
    else:
        cause = (
            "Google rejected the service-account credentials. Check that the "
            "account still exists and that its key is current."
        )

    return (
        f"Google rejected the service-account credentials.\n\n"
        f"client_email: {client_email}\n"
        f"project_id: {project_id}\n\n"
        f"{cause}\n\n"
        f"Original error: {detail}"
    )


def get_worksheets(spreadsheet_id, sourcing_tab, control_tab, partner_candidates):
    gc = get_client()
    data = load_service_account_info()
    client_email = data.get("client_email", "unknown")

    try:
        sh = gc.open_by_key(spreadsheet_id)
    except RefreshError as exc:
        raise RuntimeError(_refresh_error_help(exc, data)) from exc
    except gspread.exceptions.SpreadsheetNotFound as exc:
        raise RuntimeError(
            f"Spreadsheet '{spreadsheet_id}' was not found, or this service "
            f"account cannot see it. Share the spreadsheet with "
            f"{client_email} as an Editor."
        ) from exc
    except gspread.exceptions.APIError as exc:
        status = getattr(getattr(exc, "response", None), "status_code", None)
        if status == 403:
            raise RuntimeError(
                f"Access denied to spreadsheet '{spreadsheet_id}'. Either share "
                f"it with {client_email} as an Editor, or enable the Google "
                f"Sheets API and Google Drive API in project "
                f"'{data.get('project_id', 'unknown')}'.\n\n"
                f"Original error: {exc}"
            ) from exc
        raise

    try:
        sourcing_ws = sh.worksheet(sourcing_tab)
    except gspread.exceptions.WorksheetNotFound:
        raise RuntimeError(
            f"Worksheet '{sourcing_tab}' not found. "
            f"Available tabs: {[w.title for w in sh.worksheets()]}"
        )

    partner_ws = None
    partner_name = None
    for name in partner_candidates:
        try:
            partner_ws = sh.worksheet(name)
            partner_name = name
            break
        except gspread.exceptions.WorksheetNotFound:
            pass

    if partner_ws is None:
        raise RuntimeError(
            f"Partner worksheet not found. Tried: {list(partner_candidates)}. "
            f"Available tabs: {[w.title for w in sh.worksheets()]}"
        )

    try:
        control_ws = sh.worksheet(control_tab)
    except gspread.exceptions.WorksheetNotFound:
        control_ws = sh.add_worksheet(title=control_tab, rows=1000, cols=10)
        control_ws.update(
            "A1:D1",
            [["Partner Name", "Last Sourced UTC", "Run Count", "Last Run Added"]],
        )

    return sh, sourcing_ws, partner_ws, control_ws, partner_name
