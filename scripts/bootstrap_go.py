#!/usr/bin/env python3
"""Install an official checksum-verified Go SDK locally, without sudo/system changes."""
import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile
from pathlib import Path
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='Report existing Go only; do not download')
    args = parser.parse_args()
    local = ROOT / '.tools/go/bin/go'
    available = shutil.which('go') or (str(local) if local.is_file() else None)
    if available:
        subprocess.run([available, 'version'], check=True)
        print('Go executable:', available)
        return 0
    if args.check:
        print('Go is not installed. Run this script without --check to install locally.')
        return 2
    if platform.system() != 'Linux':
        raise SystemExit('Automatic bootstrap currently supports Linux. Install Go from https://go.dev/dl/ on this platform.')
    arch = {'x86_64':'amd64', 'aarch64':'arm64', 'arm64':'arm64'}.get(platform.machine().lower())
    if not arch:
        raise SystemExit('Unsupported CPU architecture; use the official Go installer.')
    with urlopen(Request('https://go.dev/dl/?mode=json', headers={'User-Agent':'nventures-go-bootstrap'}), timeout=30) as response:
        releases = json.load(response)
    package = next((f for release in releases if release.get('stable') for f in release['files'] if f['os']=='linux' and f['arch']==arch and f['kind']=='archive'), None)
    if not package or not re.fullmatch(r'go[0-9]+\.[0-9]+(?:\.[0-9]+)?\.linux-(amd64|arm64)\.tar\.gz', package['filename']):
        raise SystemExit('No valid stable package found in the official manifest.')
    tools = ROOT / '.tools'
    tools.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='go-download-', dir=tools) as temp:
        archive = Path(temp) / package['filename']
        digest = hashlib.sha256()
        with urlopen('https://go.dev/dl/' + package['filename'], timeout=90) as response, archive.open('wb') as output:
            while chunk := response.read(1024 * 1024):
                digest.update(chunk)
                output.write(chunk)
        if digest.hexdigest() != package['sha256']:
            raise SystemExit('Go package checksum mismatch; nothing installed.')
        staging = Path(temp) / 'unpacked'
        staging.mkdir()
        with tarfile.open(archive) as tar:
            # Python 3.12 data filtering rejects unsafe links and traversal paths.
            tar.extractall(staging, filter='data')
        if (tools / 'go').exists():
            raise SystemExit('A local Go directory already exists; preserve it and resolve manually.')
        (staging / 'go').rename(tools / 'go')
        (tools / 'go-install.json').write_text(json.dumps({'filename':package['filename'], 'sha256':package['sha256'], 'source':'https://go.dev/dl/?mode=json'}, indent=2)+'\n')
    subprocess.run([str(local), 'version'], check=True)
    print('Go is ready. Add this directory to the task PATH:', local.parent)
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
