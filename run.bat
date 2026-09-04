@echo off
rem ===========================================================================
rem  run.bat - sample launcher for mymstsc
rem
rem  Copy this next to mymstsc.exe, edit the block marked "edit these", and
rem  run it. Nothing has to be installed: mymstsc.exe hosts the Remote Desktop
rem  client control that is already part of Windows.
rem
rem  Run "mymstsc.exe /?" for the full list of options.
rem ===========================================================================

setlocal EnableExtensions

rem mymstsc.exe is expected next to this script.
set "MYMSTSC=%~dp0mymstsc.exe"

rem --- edit these ------------------------------------------------------------

rem Server, optionally with a port: "rds01", "rds01:3390", "10.0.0.5".
set "SERVER=rds01.corp.example"

rem User name: "DOMAIN\user" or "user@domain". Leave empty to be asked by
rem Windows for the whole credential.
set "RDPUSER=CORP\alice"

rem Window size, or /f for full screen, or /multimon for all monitors.
set "GEOMETRY=/w:1600 /h:900"

rem Anything else. Redirection is off unless asked for.
set "OPTIONS=/clipboard:1 /drives:0 /printers:0"

rem Keep the window open at the end when this script is double-clicked.
set "PAUSE_ON_EXIT=1"

rem --- end of edits ----------------------------------------------------------

if not exist "%MYMSTSC%" (
    echo Cannot find "%MYMSTSC%".
    echo Put run.bat in the same folder as mymstsc.exe, or edit MYMSTSC above.
    exit /b 1
)

set "CREDENTIALS="
if defined RDPUSER set "CREDENTIALS=/u:%RDPUSER% /p:-"

rem "/p:-" reads the password from this console with the echo turned off.
rem A password written into this file, or passed as /p:secret, would be visible
rem to every process on the machine through the command line.
rem
rem For an unattended run, set MYMSTSC_PASSWORD in the environment instead and
rem drop the "/p:-" above:
rem     set "MYMSTSC_PASSWORD=..."
rem     set "CREDENTIALS=/u:%RDPUSER%"

echo Connecting to %SERVER% ...
"%MYMSTSC%" /v:%SERVER% %CREDENTIALS% %GEOMETRY% %OPTIONS%
set "RC=%ERRORLEVEL%"

if %RC% equ 0 echo Session ended.
if %RC% equ 1 echo The connection failed. Run with /log:debug for details.
if %RC% equ 2 echo Logon failed: check the user name, domain and password.

if defined PAUSE_ON_EXIT pause
exit /b %RC%

rem ===========================================================================
rem  Other things you can do
rem ===========================================================================
rem
rem  A saved connection file, with switches overriding what is in it:
rem      mymstsc.exe "\\fileserver\share\prod-jump.rdp" /f
rem
rem  Full screen across every monitor:
rem      mymstsc.exe /v:rds01 /multimon
rem
rem  A high-DPI laptop: the scale follows the monitor by default; pin it with
rem      mymstsc.exe /v:rds01 /scale:150
rem
rem  The administrative session, as "mstsc /admin" would:
rem      mymstsc.exe /v:rds01 /admin
rem
rem  Through a Remote Desktop Gateway:
rem      mymstsc.exe /v:rds01 /g:gw.corp.example /gu:CORP\alice /gp:-
rem
rem  Start one program instead of the desktop:
rem      mymstsc.exe /v:rds01 /shell:C:\Windows\System32\cmd.exe
rem
rem  See which control classes this machine offers, and what gets used:
rem      mymstsc.exe /list-classes
rem      mymstsc.exe /v:rds01 /log:debug
