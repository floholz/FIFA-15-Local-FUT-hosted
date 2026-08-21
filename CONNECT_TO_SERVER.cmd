@echo off
setlocal EnableExtensions
cd /d "%~dp0"
if not exist "%~dp0payload\CONNECT_TO_SERVER.cmd" (
  echo ERROR: payload\CONNECT_TO_SERVER.cmd is missing.
  pause
  exit /b 1
)
call "%~dp0payload\CONNECT_TO_SERVER.cmd"
