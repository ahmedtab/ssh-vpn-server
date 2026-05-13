@echo off
REM Build script for LinkThings Client - Windows amd64
REM This script should be run on Windows with Go installed
REM Usage: build.bat [version] (default: 1.0.0.0)

setlocal enabledelayedexpansion

REM Check if Go is installed
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo Error: Go is not installed or not in PATH
    exit /b 1
)

REM Set version from argument or default
set VERSION=%1
if "!VERSION!"=="" set VERSION=1.0.0.0

REM Set output directory
set OUTPUT_DIR=.\dist
if not exist !OUTPUT_DIR! mkdir !OUTPUT_DIR!

REM Build executable
echo Building LinkThings Client v!VERSION! for Windows amd64...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

REM Build with version information embedded
go build -o !OUTPUT_DIR!\linkthings-client.win.amd64.exe ^
    -ldflags "-X main.Version=!VERSION!" ^
    .

if %errorlevel% neq 0 (
    echo Build failed!
    exit /b 1
)

echo Build completed successfully!
echo Output: !OUTPUT_DIR!\linkthings-client.win.amd64.exe
exit /b 0
