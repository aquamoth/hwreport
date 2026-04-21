@echo off
setlocal
title Hardware Report

set "HERE=%~dp0"
set "HERE=%HERE:~0,-1%"

rem Launch PowerShell detached so cmd.exe exits immediately and releases
rem its handle on this .cmd file. The script is loaded via Get-Content +
rem Invoke-Expression (not -File) so PowerShell never holds a handle on
rem run-report.ps1 either. Net result: hwreport.exe runs straight from
rem the USB stick and nothing is copied to the local machine.
start "Hardware Report" powershell -NoProfile -ExecutionPolicy Bypass -Command "$env:HWREPORT_USB='%HERE%'; Invoke-Expression (Get-Content -Raw -LiteralPath ($env:HWREPORT_USB + '\run-report.ps1'))"

endlocal
