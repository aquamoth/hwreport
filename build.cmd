@echo off
setlocal

set "GO_BIN=C:\Program Files\Go\bin"
if exist "%GO_BIN%\go.exe" (
    set "PATH=%GO_BIN%;%PATH%"
)

powershell -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
exit /b %ERRORLEVEL%
