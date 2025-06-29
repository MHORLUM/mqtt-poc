@echo off
REM Batch script to build MQTT server as Windows executable

echo Building MQTT Server as Windows executable...

REM Set environment variables for Windows build
set GOOS=windows
set GOARCH=amd64

REM Create bin directory if it doesn't exist
if not exist "bin" (
    mkdir bin
    echo Created bin directory
)

REM Build the server
echo Compiling server...
go build -o bin/server.exe ./cmd/server

if %ERRORLEVEL% EQU 0 (
    echo.
    echo ✓ Server executable built successfully: bin/server.exe
    echo.
    echo To run the server:
    echo   .\bin\server.exe
) else (
    echo.
    echo ✗ Build failed!
    exit /b 1
)

REM Reset environment variables
set GOOS=
set GOARCH=

pause
