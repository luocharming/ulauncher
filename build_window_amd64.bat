@echo off

:: 注意：客户端前端不在此手动构建。
:: wailsjs/go 下的 Go->JS 绑定只能由 wails 生成（该目录已 gitignore），
:: 而 npm run build 依赖这些绑定。wails build 会先生成绑定、再构建 client 前端，
:: 故 client 前端交由下方的 wails build 统一处理，避免绑定过期导致构建失败。

:: 构建服务端前端
echo Building server frontend...
cd server/frontend
call npm run build
if %errorlevel% neq 0 (
    echo Server frontend build failed!
    exit /b %errorlevel%
)
cd ../..

:: 设置应用版本
set AppVersion=0.0.18

:: 获取 Git 提交哈希
for /f "delims=" %%i in ('git rev-parse HEAD') do set GitCommit=%%i

:: 获取当前日期和时间
for /f "delims=" %%i in ('powershell -Command "Get-Date -Format 'yyyy-MM-dd HH:mm:ss'"') do set BuildDate=%%i

:: 设置目标平台环境变量
set GOOS=windows
set GOARCH=amd64

:: 执行 Wails 构建（会先生成 Go->JS 绑定，再构建 client 前端并编译）
:: -m 跳过 go mod tidy，避免联网拉取测试依赖失败
echo Building wails app...
wails build -m -ldflags "-X main.GitCommit=%GitCommit% -X 'main.BuildDate=%BuildDate%' -X main.AppVersion=%AppVersion%" -o thingue-launcher-v%AppVersion%.exe
