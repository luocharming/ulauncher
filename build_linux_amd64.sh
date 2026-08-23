# $AppVersion = "0.0.12"
# $GitCommit = git rev-parse HEAD
# $BuildDate = Get-Date -Format 'yyyy-MM-dd HH:mm:ss'
# wails build -ldflags "-X main.GitCommit=$GitCommit -X 'main.BuildDate=$BuildDate' -X main.AppVersion=$AppVersion" -o thingue-launcher-v$AppVersion
# 构建客户端前端
echo "Building client frontend..."
cd client/frontend
npm run build
if [ $? -ne 0 ]; then
    echo "Client frontend build failed!"
    exit 1
fi
cd ../..

# 构建服务端前端
echo "Building server frontend..."
cd server/frontend
npm run build
if [ $? -ne 0 ]; then
    echo "Server frontend build failed!"
    exit 1
fi
cd ../..

export AppVersion="0.0.18"
wails build -ldflags "-X main.GitCommit=`git rev-parse HEAD` -X 'main.BuildDate=`date "+%Y-%m-%d %H:%M:%S"`' -X main.AppVersion=0.0.4 -X main.AppVersion=$AppVersion" -o thingue-launcher-v$AppVersion
