@echo off
setlocal EnableExtensions
cd /d "%~dp0"
title FIFA 15 Local FUT - Hosted Server

echo ============================================================
echo  FIFA 15 LOCAL FUT - HOSTED SERVER (for friends)
echo ============================================================
echo  This hosts the FUT backend for other players. Friends run
echo  CONNECT_TO_SERVER.cmd with the address shown below.
echo  Use a VPN (Tailscale/ZeroTier/Radmin) - traffic is not encrypted.
echo.

where py.exe >nul 2>nul
if not errorlevel 1 (
    set "PY=py -3"
) else (
    where python.exe >nul 2>nul
    if errorlevel 1 (
        echo ERROR: Python 3 is not installed.
        echo Run INSTALL_PREREQUISITES.cmd from the release package, then try again.
        pause
        exit /b 1
    )
    set "PY=python"
)

call "%~dp0STOP_LOCAL_FUT15.cmd" /quiet >nul 2>nul

set "PUBLIC=%~1"
if not defined PUBLIC (
    set /p "PUBLIC=Address players should connect to [empty = auto-detect]: "
)
if defined PUBLIC (
    %PY% "%~dp0localfut15\server.py" --mode server --public-host "%PUBLIC%"
) else (
    %PY% "%~dp0localfut15\server.py" --mode server
)
set "RC=%ERRORLEVEL%"
echo.
echo Local FUT server exited with code %RC%.
pause
exit /b %RC%
