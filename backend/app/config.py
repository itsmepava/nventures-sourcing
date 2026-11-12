import os
from pathlib import Path
from dotenv import load_dotenv

ROOT = Path(__file__).resolve().parents[2]
load_dotenv(ROOT / '.env')
MODE = os.getenv('APP_MODE', 'demo')
if MODE not in ('demo', 'live'):
    raise RuntimeError('APP_MODE must be demo or live')
DATABASE_URL = os.getenv('DATABASE_URL', f'sqlite:///{ROOT / ("nventures-demo.db" if MODE == "demo" else "nventures.db")}')
SYNC_ENABLED = MODE == 'live' and os.getenv('SHEETS_SYNC_ENABLED', 'false').lower() == 'true'
MAX_RESEARCH = int(os.getenv('MAX_RESEARCH_PER_RUN', '45'))

