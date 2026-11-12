param([string]$Python = 'python', [switch]$RebuildFrontend)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
& $Python -m venv .venv
if ($LASTEXITCODE -ne 0) { throw 'Install Python 3.11+ or pass -Python with the full path to python.exe.' }
$appPython = Join-Path $PSScriptRoot '.venv/Scripts/python.exe'
& $appPython -m pip install -r backend/requirements.txt
if ($LASTEXITCODE -ne 0) { throw 'Python dependency installation failed.' }
if (!(Test-Path -LiteralPath '.env')) { Copy-Item -LiteralPath '.env.example' -Destination '.env' }
if ($RebuildFrontend -or !(Test-Path -LiteralPath 'frontend/dist/index.html')) {
    Push-Location frontend
    try {
        & npm.cmd ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed.' }
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    } finally { Pop-Location }
} else { Write-Host 'Using the bundled frontend. Node.js is only needed if you rebuild it.' }
Write-Host 'Setup complete. Run start.ps1 or double-click Start.cmd to open the app.'
