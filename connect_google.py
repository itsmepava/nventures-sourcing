"""One-time Google Desktop OAuth setup, using only Python's standard library."""
import base64
import hashlib
import json
import secrets
import time
import webbrowser
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import urlencode, urlparse, parse_qs
from urllib.request import Request, urlopen

def main():
    root = Path(__file__).resolve().parent
    credential_path = Path(input('Path to Google Desktop OAuth client JSON: ').strip().strip('"'))
    client = json.loads(credential_path.read_text(encoding='utf-8')).get('installed')
    if not client:
        raise SystemExit('Use a Desktop app OAuth client JSON from Google Cloud.')
    state, verifier = secrets.token_urlsafe(32), secrets.token_urlsafe(64)
    result = {}
    class Callback(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass
        def do_GET(self):
            values = parse_qs(urlparse(self.path).query)
            if not secrets.compare_digest(values.get('state', [''])[0], state):
                self.send_error(400)
                return
            result.update(values)
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.end_headers()
            self.wfile.write(b'Google authorization received. Return to the setup window.')
    with HTTPServer(('127.0.0.1', 0), Callback) as server:
        server.timeout = 1
        redirect = f'http://127.0.0.1:{server.server_port}'
        challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip('=')
        webbrowser.open('https://accounts.google.com/o/oauth2/v2/auth?' + urlencode(dict(client_id=client['client_id'], redirect_uri=redirect, response_type='code', scope='https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/spreadsheets', access_type='offline', prompt='consent', state=state, code_challenge=challenge, code_challenge_method='S256')))
        deadline = time.monotonic() + 300
        while not result and time.monotonic() < deadline:
            server.handle_request()
    if not result.get('code'):
        raise SystemExit('Authorization cancelled or expired. No credentials saved.')
    try:
        payload = urlencode(dict(client_id=client['client_id'], client_secret=client['client_secret'], code=result['code'][0], code_verifier=verifier, redirect_uri=redirect, grant_type='authorization_code')).encode()
        with urlopen(Request('https://oauth2.googleapis.com/token', data=payload), timeout=30) as response:
            token = json.load(response)['refresh_token']
        from dotenv import set_key
        for key, value in dict(GOOGLE_OAUTH_CLIENT_ID=client['client_id'], GOOGLE_OAUTH_CLIENT_SECRET=client['client_secret'], GOOGLE_OAUTH_REFRESH_TOKEN=token).items():
            set_key(str(root / '.env'), key, value)
    except Exception:
        raise SystemExit('Google setup failed. No token is shown in logs. Try again.') from None
    print('Google connected. Restart the app and turn Safe demo off in Pitch inbox.')

if __name__ == '__main__':
    main()
