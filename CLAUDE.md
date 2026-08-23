# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

ThingUE启动器是一个基于 Wails v2 框架的桌面应用程序，用于管理和启动虚幻引擎（Unreal Engine）实例。项目包含 GUI 应用和 CLI 工具两个入口。

**技术栈**：
- 后端：Go 1.21 + Wails v2 + Gin + GORM + SQLite
- 前端：Vue 3 + Quasar UI + Vite
- 消息队列：MQTT
- CLI框架：Cobra

## 构建命令

### GUI 应用构建

**Windows (AMD64)**：
```bash
build_window_amd64.bat
```
或手动执行：
```bash
wails build -ldflags "-X main.GitCommit=<commit> -X 'main.BuildDate=<date>' -X main.AppVersion=<version>"
```

**Linux (AMD64)**：
```bash
./build_linux_amd64.sh
```

### CLI 工具构建

**Windows (AMD64)**：
```bash
build_window_cli_amd64.bat
```
输出：`build/bin/cli.exe`

### 前端开发

进入前端目录：
```bash
cd client/frontend
npm install
npm run dev      # 开发模式
npm run build    # 生产构建
```

### Wails 开发模式

```bash
wails dev
```

## 项目架构

### 目录结构

```
thing.ue.launcher/
├── main.go                    # GUI应用主入口
├── cli/                       # CLI工具
│   ├── main.go               # CLI主程序
│   ├── instance.go           # 实例管理命令
│   ├── agent.go              # 代理命令
│   └── server.go             # 服务器命令
├── client/                    # GUI客户端
│   ├── api/                  # Wails绑定的API
│   ├── frontend/             # Vue3前端
│   ├── service/              # 业务逻辑服务
│   │   ├── instance/         # 实例管理
│   │   └── server/           # 服务器连接
│   └── initialize/           # 初始化模块
├── server/                    # 后端服务
│   ├── core/                 # 核心业务逻辑
│   │   ├── service/          # 服务层
│   │   └── provider/         # 连接管理
│   ├── web/                  # Web路由和处理器
│   │   ├── handler/          # HTTP/WebSocket/MQTT处理器
│   │   └── router/           # 路由定义
│   ├── frontend/             # 服务端前端（独立Web界面）
│   └── initialize/           # 服务初始化
└── common/                    # 共享代码
    ├── model/                # 数据模型
    ├── message/              # 消息定义
    ├── domain/               # 业务域模型
    ├── constants/            # 常量定义
    ├── provider/             # 配置和资源提供者
    ├── logger/               # 日志模块
    └── util/                 # 工具函数
```

### 核心模块

#### 1. 实例管理模块
- **客户端**：`client/service/instance/instance_manager.go` - 管理本地UE实例的创建、配置、启动、停止
- **服务端**：`server/core/service/instance_service.go` - 管理服务器端实例状态
- **进程管理**：`client/service/instance/runner_manager.go` - 管理UE进程的运行
- **跨平台命令**：`client/service/instance/os_cmd/` - Windows/Linux命令执行适配

#### 2. 服务器通信模块
- **连接管理**：`client/service/server/conn_manager.go` - 管理客户端与服务器的WebSocket连接
- **客户端连接提供者**：`server/core/provider/client_conn_provider.go` - 管理服务器端的客户端连接
- **管理员连接提供者**：`server/core/provider/admin_conn_provider.go` - 管理管理员连接
- **流媒体连接器**：`server/core/provider/streamer_connector.go` - 管理UE Pixel Streaming连接

#### 3. 消息系统
- **消息定义**：`common/message/client_server.go` - 定义客户端-服务器消息协议
- **消息类型**：`common/message/types/types.go` - 消息类型枚举
- **主要消息**：
  - `ServerProcessControl` - 服务器发送的进程控制命令
  - `ClientProcessStateUpdate` - 客户端上报的进程状态
  - `ServerStreamerConnectedUpdate` - 流媒体连接状态更新

#### 4. 数据库模块
- **ORM**：GORM
- **数据库**：SQLite
- **初始化**：
  - 客户端：`client/initialize/gorm_initialize.go`
  - 服务端：`server/initialize/gorm.go`
- **模型**：
  - `common/model/instance_client.go` - 客户端实例模型
  - `common/model/instance_server.go` - 服务器实例模型
  - `common/model/client.go` - 客户端模型

#### 5. 日志模块
- **实现**：`common/logger/zap_logger.go` - 基于Zap的日志封装
- **初始化**：
  - 客户端：`client/initialize/zap_logger.go`
  - 服务端：`server/initialize/zap.go`

### 数据流

```
GUI前端 (Vue3)
    ↓ (Wails绑定)
client/api (Go函数)
    ↓
client/service (业务逻辑)
    ↓ (WebSocket/MQTT)
server/web/handler (HTTP/WebSocket处理器)
    ↓
server/core/service (服务端业务逻辑)
    ↓
数据库 (SQLite)
```

## 配置文件

### wails.json
Wails框架配置，定义前端目录、构建命令、应用信息等。

### thingue-launcher/config.yaml
应用运行时配置：
- `serverURL` - 服务器地址
- `localServer` - 本地服务器配置（端口、静态文件路径等）
- `systemSettings` - 系统设置（日志级别、外部编辑器路径等）
- `peerConnectionOptions` - WebRTC连接选项

### go.mod
Go依赖管理，主要依赖：
- `github.com/wailsapp/wails/v2` - Wails框架
- `github.com/gin-gonic/gin` - Web框架
- `gorm.io/gorm` - ORM
- `github.com/mochi-mqtt/server/v2` - MQTT服务器
- `github.com/spf13/cobra` - CLI框架
- `go.uber.org/zap` - 日志库

## 开发注意事项

### Wails API 绑定
- 客户端API位于 `client/api/`
- 需要在 `main.go` 中通过 `app.Bind()` 绑定API结构体
- 前端通过 `window.go.<namespace>.<method>()` 调用Go函数

### 跨平台支持
- 进程执行命令需要区分Windows和Linux：`client/service/instance/os_cmd/`
- 使用 `runtime.GOOS` 判断操作系统
- 构建脚本：`.bat` (Windows) 和 `.sh` (Linux)

### 消息协议
- 客户端-服务器通信使用自定义消息协议
- 消息类型定义在 `common/message/types/types.go`
- 消息结构定义在 `common/message/client_server.go`
- 使用WebSocket和MQTT两种传输方式

### 实例管理
- 客户端实例（ClientInstance）：存储在客户端数据库，包含本地配置
- 服务器实例（ServerInstance）：存储在服务器数据库，包含运行状态
- 实例状态通过消息系统同步

### 日志
- 使用Zap日志库
- 日志级别可在 `config.yaml` 中配置
- 日志文件轮转使用 `lumberjack`

### 前端开发
- 使用Vue 3 Composition API
- UI组件库：Quasar
- 代码编辑器：Monaco Editor
- 主要组件：
  - `client/frontend/src/components/unreal/` - UE实例管理界面
  - `client/frontend/src/components/server/` - 服务器管理界面
  - `client/frontend/src/components/Setting.vue` - 设置界面

### 版本管理
- 版本号在构建脚本中设置（`AppVersion`变量）
- Git提交哈希和构建时间通过 `-ldflags` 注入
- 版本信息可通过 `cli/version.go` 查看
