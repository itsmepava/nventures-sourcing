#!/usr/bin/env python3
"""Check the recovered baseline without live provider requests or modifying user data."""
import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--build', action='store_true', help='Also rebuild the existing frontend bundle')
    args = parser.parse_args()
    python = ROOT / '.venv' / ('Scripts/python.exe' if os.name=='nt' else 'bin/python')
    if not python.is_file():
        raise SystemExit('Current app environment is missing. Use setup.sh/setup.ps1 with a Python that supports venv.')
    node = shutil.which('node')
    bundled_node = Path.home() / '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node'
    if not node and bundled_node.is_file():
        node = str(bundled_node)
    print('App root:', ROOT, flush=True)
    print('Go available:', bool(shutil.which('go') or (ROOT/'.tools/go/bin/go').is_file()), flush=True)
    print('Current mode defaults to demo; tests use isolated fixtures and databases.', flush=True)
    env = dict(os.environ)
    env['APP_MODE'] = 'demo'
    subprocess.run([str(python), '-m', 'unittest', 'discover', '-s', 'tests', '-v'], cwd=ROOT/'backend', env=env, check=True)
    if not node or not (ROOT/'frontend/node_modules/vitest/vitest.mjs').is_file():
        raise SystemExit('Backend checks passed; frontend tooling missing. Install locked frontend dependencies, then rerun. No live requests made.')
    env['PATH'] = str(Path(node).parent) + os.pathsep + env.get('PATH','')
    subprocess.run([node, 'node_modules/vitest/vitest.mjs', 'run'], cwd=ROOT/'frontend', env=env, check=True)
    if args.build:
        subprocess.run([node, 'node_modules/typescript/bin/tsc', '-b'], cwd=ROOT/'frontend', env=env, check=True)
        subprocess.run([node, 'node_modules/vite/bin/vite.js', 'build'], cwd=ROOT/'frontend', env=env, check=True)
    print('Baseline preflight passed. No Gmail, Sheets, Tavily or OpenRouter live actions were performed.', flush=True)
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
