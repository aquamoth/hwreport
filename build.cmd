@echo off
setlocal

go run ./cmd/buildtool %*
exit /b %ERRORLEVEL%
