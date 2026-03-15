@echo off
setlocal EnableDelayedExpansion

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
echo Binary:     %BINARY_NAME%
echo ==========================================

REM 删除旧的二进制文件
if exist %BINARY_NAME% (
    echo Removing old %BINARY_NAME%...
    del /f %BINARY_NAME% 2>nul
)

echo Building...
go build -ldflags="-s -w -X main.version=%VERSION% -X main.buildTime=%BUILD_TIME% -X main.isRelease=true" -o %BINARY_NAME% ./cmd/udphub

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

endlocal
