#!/usr/bin/env sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
if [ ! -x .venv/bin/python ]; then
  printf '%s\n' 'Run sh setup.sh once first.'
  exit 1
fi
exec .venv/bin/python launch.py
