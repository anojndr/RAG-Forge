@echo off
setlocal

echo.
echo ==========================================================
echo  Starting RAG-Forge Services for Windows (Extract Only)
echo ==========================================================
echo.

REM --- RAG-Forge (Go) ---
echo.
echo [1/1] Starting RAG-Forge Go application (Extract Endpoint Only)...
go run main.go --endpoint=extract

echo.
echo Go application has stopped.
pause