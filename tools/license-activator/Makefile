APP_NAME := license-activator
DIST_DIR := dist

.PHONY: all build-win build-linux clean deps

all: build-win build-linux

$(DIST_DIR):
	@mkdir -p $(DIST_DIR)

# 拉取并整理依赖，生成 go.sum
deps:
	go mod tidy

# Windows amd64 构建（需安装 GNU Make，并在支持 CGO 的环境运行）
build-win: deps $(DIST_DIR)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o $(DIST_DIR)/$(APP_NAME)-windows-amd64.exe .

# Linux amd64 构建（建议在 Linux/WSL 下运行）
build-linux: deps $(DIST_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o $(DIST_DIR)/$(APP_NAME)-linux-amd64 .

clean:
	rm -rf $(DIST_DIR)
