@echo off
setlocal

:: ============================================================
:: X Launcher all-in-one build script (Windows host)
::   1. Build client/frontend  (npm run build)
::   2. Build server/frontend  (npm run build)
::   3. Build Windows CLI      -> build\bin\cli.exe
::   4. Cross-build Linux CLI  -> build\bin\cli      (zig + CGO)
::   5. Build Windows GUI      -> build\bin\x-launcher-v{VERSION}.exe (wails)
::
:: Usage:  build_all_amd64.bat [version]    (default 1.0.1)
:: Requires: node/npm, go, swag, wails, zig (D:\tools\zig)
:: ============================================================

:: App version (override via first argument)
set AppVersion=1.0.1
if not "%~1"=="" set AppVersion=%~1

:: Git commit hash
for /f "delims=" %%i in ('git rev-parse HEAD') do set GitCommit=%%i

:: Build time
for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "Get-Date -Format 'yyyy-MM-dd HH:mm:ss'"`) do set BuildDate=%%i

:: Generate Swagger docs (must run in native env BEFORE setting GOOS=linux)
where swag >nul 2>nul || go install github.com/swaggo/swag/cmd/swag@latest
if errorlevel 1 exit /b 1
swag init -g server/web/router/doc.go --output server/docs --parseDependency --parseInternal
if errorlevel 1 exit /b 1

:: 1. Build client frontend
echo [1/5] Building client/frontend ...
pushd client\frontend
call npm run build
if errorlevel 1 (popd & exit /b 1)
popd

:: 2. Build server frontend
echo [2/5] Building server/frontend ...
pushd server\frontend
call npm run build
if errorlevel 1 (popd & exit /b 1)
popd

:: Output dir
if not exist build\bin mkdir build\bin

set LDFLAGS=-X main.GitCommit=%GitCommit% -X 'main.BuildDate=%BuildDate%' -X main.AppVersion=%AppVersion%

:: 3. Windows CLI (native env, keep machine default CGO settings)
echo [3/5] Building Windows CLI (cli.exe) ...
set GOOS=windows
set GOARCH=amd64
set CC=
set CXX=
go build -o build\bin\cli.exe -ldflags "%LDFLAGS%" ./cli
if errorlevel 1 exit /b 1

:: 4. Linux CLI (CGO cross-compile, zig as C/C++ cross compiler)
echo [4/5] Building Linux CLI (cli) ...
where zig >nul 2>nul || set PATH=D:\tools\zig;%PATH%
where zig >nul 2>nul || (echo [ERROR] zig not found, install it or put it under D:\tools\zig & exit /b 1)
set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=1
set CC=zig cc -target x86_64-linux-gnu.2.17
set CXX=zig c++ -target x86_64-linux-gnu.2.17
go build -o build\bin\cli -ldflags "%LDFLAGS%" ./cli
if errorlevel 1 exit /b 1

:: 5. Windows GUI (wails; frontend already built so use -s to skip rebuild; restore native env)
echo [5/5] Building Windows GUI (x-launcher-v%AppVersion%.exe) ...
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=
set CC=
set CXX=
wails build -s -ldflags "%LDFLAGS%" -o x-launcher-v%AppVersion%.exe
if errorlevel 1 exit /b 1

echo.
echo ============================================
echo  All builds completed:
echo    build\bin\cli.exe                    (Windows CLI)
echo    build\bin\cli                        (Linux CLI)
echo    build\bin\x-launcher-v%AppVersion%.exe  (Windows GUI)
echo ============================================
endlocal
