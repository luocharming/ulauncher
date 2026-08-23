
@echo off  

:: 构建客户端前端
echo Building client frontend...
cd client/frontend
call npm run build
if %errorlevel% neq 0 (
    echo Client frontend build failed!
    exit /b %errorlevel%
)
cd ../..

:: 构建服务端前端
echo Building server frontend...
cd server/frontend
call npm run build
if %errorlevel% neq 0 (
    echo Server frontend build failed!
    exit /b %errorlevel%
)
cd ../..

:: Set application version  
set AppVersion=0.0.16 

:: Get Git commit hash  
for /f "delims=" %%i in ('git rev-parse HEAD') do set GitCommit=%%i  

:: Get current date and time  
for /f "delims=" %%i in ('powershell -Command "Get-Date -Format ''yyyy-MM-dd HH:mm:ss''"') do set BuildDate=%%i  

:: Set target platform environment variables  
set GOOS=windows
set GOARCH=amd64

:: Execute build command  
go build -o build\bin\cli.exe -ldflags "-X main.GitCommit=%GitCommit% -X 'main.BuildDate=%BuildDate%' -X main.AppVersion=%AppVersion%" ./cli  

echo Build completed: build\bin\cli.exe  