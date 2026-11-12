@echo off
rem Start the nVentures Go app on Windows. Place nventures.exe next to this file
rem (or in .\bin\). Configuration is read from .env in this folder if present.
cd /d "%~dp0"
set "BIN=%~dp0bin\nventures.exe"
if not exist "%BIN%" set "BIN=%~dp0nventures.exe"
if not exist "%BIN%" (
  echo nventures.exe was not found. Download the Windows build from the release page.
  pause
  exit /b 1
)
if exist ".env" (
  "%BIN%" serve --env ".env"
) else (
  "%BIN%" serve --env ""
)
pause
