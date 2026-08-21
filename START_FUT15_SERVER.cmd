@echo off
setlocal EnableExtensions
cd /d "%~dp0"
if not exist "%~dp0payload\START_FUT15_SERVER.cmd" (
  echo ERROR: payload\START_FUT15_SERVER.cmd is missing.
  pause
  exit /b 1
)
call "%~dp0payload\START_FUT15_SERVER.cmd" %*
