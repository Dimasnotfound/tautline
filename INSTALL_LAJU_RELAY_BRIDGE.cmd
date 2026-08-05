@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\install-laju-relay-bridge.ps1"
exit /b %ERRORLEVEL%
