"""One Python process serves the API and interface, then opens the browser."""
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
import webbrowser
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parent
URL = 'http://127.0.0.1:8000'

def main():
    local_python = ROOT / '.venv' / ('Scripts/python.exe' if os.name == 'nt' else 'bin/python')
    if local_python.is_file() and Path(sys.executable).resolve() != local_python.resolve():
        return subprocess.call([str(local_python), str(Path(__file__).resolve())])
    try:
        import uvicorn
    except ImportError:
        print('Run setup.ps1 (Windows) or sh setup.sh (Linux) once first to install the app dependencies.')
        return 1
    if not (ROOT / 'frontend' / 'dist' / 'index.html').is_file():
        print('The interface needs to be built. Run setup.ps1 (Windows) or sh setup.sh (Linux) once first.')
        return 1
    def is_running():
        try:
            import json
            with urlopen(URL + '/api/health', timeout=1) as response:
                return json.load(response).get('app') == 'nventures-sourcing-2'
        except Exception:
            return False
    if is_running():
        webbrowser.open(URL)
        print('nVentures is already running. Opened your browser.')
        return 0
    os.chdir(ROOT / 'backend')
    sys.path.insert(0, str(ROOT / 'backend'))
    def open_when_ready():
        for _ in range(60):
            if is_running():
                webbrowser.open(URL)
                return
            time.sleep(.25)
    threading.Thread(target=open_when_ready, daemon=True).start()
    print(f'Opening nVentures at {URL}. Keep this window open; press Ctrl+C to stop.')
    uvicorn.run('app.main:app', host='127.0.0.1', port=8000, workers=1, access_log=False)
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
