Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Join-Path $PSScriptRoot 'frontend')
if (!(Test-Path -LiteralPath 'node_modules')) { throw 'Run setup.ps1 first to install frontend dependencies.' }
& npm.cmd run dev
