@echo off
setlocal EnableExtensions
cd /d "%~dp0"
title FIFA 15 Local FUT - Connect To Server

echo ============================================================
echo  FIFA 15 LOCAL FUT - CONNECT TO A HOSTED SERVER
echo ============================================================
echo  Enter the address (IP or hostname) of a friend's Local FUT
echo  server, e.g. a Tailscale/ZeroTier/Radmin VPN address.
echo  Leave it empty to go back to the normal offline/local mode.
echo.

where py.exe >nul 2>nul
if not errorlevel 1 (
    set "PY=py -3"
) else (
    where python.exe >nul 2>nul
    if errorlevel 1 (
        echo ERROR: Python 3 was not found.
        echo Run INSTALL_PREREQUISITES.cmd from the release package first.
        pause
        exit /b 1
    )
    set "PY=python"
)

%PY% "%~dp0localfut15\hosted_config.py" show
echo.
set "SERVER="
set /p "SERVER=Server address [empty = local mode]: "
if not defined SERVER (
    %PY% "%~dp0localfut15\hosted_config.py" local
) else (
    set "PLAYER="
    set /p "PLAYER=Your player name on that server [empty = keep current / Windows user name]: "
    if defined PLAYER (
        %PY% "%~dp0localfut15\hosted_config.py" client "%SERVER%" "%PLAYER%"
    ) else (
        %PY% "%~dp0localfut15\hosted_config.py" client "%SERVER%"
    )
)
echo.
echo Done. Start the game with PLAY_LOCAL_FUT15.cmd as usual.
pause
endlocal
