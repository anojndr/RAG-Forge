@echo off
setlocal

echo.
echo ==========================================================
echo  Starting RAG-Forge Services for Windows
echo ==========================================================
echo.

REM Attempt to add this script's directory to Windows Defender exclusions.
REM This requires running the batch file as an Administrator.
echo Attempting to add project directory to Windows Defender exclusions...
powershell -Command "Start-Process powershell -ArgumentList '-NoProfile -Command Add-MpPreference -ExclusionPath \"%~dp0\"' -Verb RunAs" -ErrorAction SilentlyContinue

REM --- Cleanup instructions ---
echo To stop all services, press Ctrl+C in this window.
echo If the Python service remains running in the background, you can find its
echo process ID (PID) with 'tasklist ^| findstr python' and stop it with
echo 'taskkill /F /PID <PID>'.
echo.
echo.

REM --- Transcript Service (Python) ---
echo [1/2] Setting up Python transcript service...
cd transcript-service

REM Create a virtual environment if it doesn't exist.
IF NOT EXIST venv (
    echo Creating Python virtual environment...
    python -m venv venv
)

REM Activate the virtual environment
call venv\Scripts\activate.bat

REM Install dependencies if they haven't been cached
IF NOT EXIST requirements.txt.cached (
    echo Installing Python requirements...
    pip install -r requirements.txt
    REM Create a cache file to skip installation next time
    copy requirements.txt requirements.txt.cached > NUL
) ELSE (
    echo Python requirements are up-to-date.
)

REM Start the Python service in a new, minimized background window
echo Starting transcript service on http://127.0.0.1:8000...
start "Transcript Service" /B uvicorn main:app --host 127.0.0.1 --port 8000
cd ..

REM Give the python service a moment to start
timeout /t 2 /nobreak > NUL

REM --- RAG-Forge (Go) ---
echo.
echo [2/2] Starting RAG-Forge Go application...
go run main.go

echo.
echo Go application has stopped.
echo You may need to manually stop the background Python service.
pause