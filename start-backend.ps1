Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
$appPython = Join-Path $PSScriptRoot '.venv/Scripts/python.exe'
if (!(Test-Path -LiteralPath $appPython)) { throw 'Run setup.ps1 first to create the local Python environment.' }
Set-Location -LiteralPath (Join-Path $PSScriptRoot 'backend')
& $appPython -m uvicorn app.main:app --host 127.0.0.1 --port 8000
