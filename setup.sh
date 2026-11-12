#!/usr/bin/env sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PYTHON="${NV_PYTHON:-python3}"
"$PYTHON" -m venv .venv
.venv/bin/python -m pip install -r backend/requirements.txt
if [ ! -f .env ]; then cp .env.example .env; fi
chmod 600 .env
if [ ! -f frontend/dist/index.html ]; then
  (cd frontend && npm ci --no-audit --no-fund && npm run build)
fi
printf '%s\n' 'Setup complete. Run: sh start.sh'
