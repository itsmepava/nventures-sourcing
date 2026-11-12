Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
$appPython = Join-Path $PSScriptRoot '.venv/Scripts/python.exe'
if (!(Test-Path -LiteralPath $appPython)) { throw 'Run setup.ps1 once first.' }
& $appPython (Join-Path $PSScriptRoot 'launch.py')
exit $LASTEXITCODE
