@echo off
setlocal EnableDelayedExpansion

REM ==========================================
REM DraARL Release Build Script
REM Frontend + Backend, multi-platform
REM ==========================================

set BINARY_NAME=DraARL.exe

for /f "delims=" %%i in ('git describe --tags --always --dirty 2^>nul') do set VERSION=%%i
if not defined VERSION set VERSION=dev

for /f "tokens=*" %%i in ('powershell -Command "Get-Date -Format yyyy-MM-ddTHH:mm:ssZ"') do set BUILD_TIME=%%i
if not defined BUILD_TIME set BUILD_TIME=unknown

echo ==========================================
echo DraARL Release Build
echo ==========================================
echo Version:    %VERSION%
echo Build Time: %BUILD_TIME%
echo ==========================================
echo.

REM Clean old build artifacts
echo [1/4] Cleaning old build artifacts...
if exist %BINARY_NAME% del /f %BINARY_NAME% 2>nul
if exist "www\dist" rmdir /s /q www\dist 2>nul
if exist "internal\server\web" rmdir /s /q internal\server\web 2>nul

echo [2/4] Building frontend...
cd www
call npm run build
if errorlevel 1 (
    echo Frontend build failed!
    cd ..
    exit /b 1
)
cd ..

echo.
echo [3/4] Copying frontend dist to internal\server\web\dist...
if not exist "internal\server\web" mkdir internal\server\web
xcopy /e /i /q /y "www\dist" "internal\server\web\dist"
if errorlevel 1 (
    echo Failed to copy frontend files!
    exit /b 1
)

echo.
echo [4/4] Building backend with embedded frontend...
set CGO_ENABLED=0
go build -ldflags="-s -w -X main.version=%VERSION% -X main.buildTime=%BUILD_TIME% -X main.isRelease=true" -tags=embed -o %BINARY_NAME% ./cmd/udphub

if %ERRORLEVEL% equ 0 (
    echo.
    echo ==========================================
    echo Build successful!
    echo ==========================================
    for %%A in (%BINARY_NAME%) do echo Size: %%~zA bytes
    echo.
    echo Version info:
    %BINARY_NAME% -v
) else (
    echo.
    echo ==========================================
    echo Build FAILED!
    echo ==========================================
    exit /b 1
)

REM Clean intermediate files (keep www\dist for development)
echo.
echo Cleaning intermediate files...
rmdir /s /q internal\server\web 2>nul

echo.
echo Done! Binary: %BINARY_NAME%

endlocal
