# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

本文件针对 `server/` 目录（ThingUE 信令服务），是仓库根目录 CLAUDE.md 的补充，根目录中的整体架构、构建命令、消息协议说明依然适用。

## 目录定位

`server/` 是 ThingUE 启动器的**信令服务端**，负责：

- **实例管理**：接收启动器客户端（`client/`）注册的 UE 实例信息，下发进程控制命令（启动/停止）
- **Pixel Streaming 信令**：转发 UE 实例（Streamer）与浏览器播放端（Player）之间的 WebRTC SDP 信令
- **管理后台**：内嵌 Vue3 管理界面（`server/frontend/`，通过 `go:embed` 打进二进制）
- **其他功能**：MQTT Broker、模型库、云端资源同步、License 授权校验

服务以两种方式启动：
1. **CLI**：`go run ./cli server`（见 `cli/server.go`，支持 `--bindAddr`、`--contentPath`、`--logLevel`、`--staticDir`、`--turn-server/--turn-username/--turn-password` 等 flag）
2. **GUI 内嵌**：Wails 应用 `main.go` 调用 `server.Init()` + `initialize.Server.Serve()`

## 构建与测试

```bash
# 运行信令服务（默认监听 0.0.0.0:8877）
go run ./cli server --logLevel debug

# 编译（完整应用构建脚本在仓库根目录，CLI 输出 build/bin/cli.exe）
./build_window_cli_amd64.bat

# 前端开发（vite 代理 /api、/ws 到 127.0.0.1:8877）
cd server/frontend && npm run dev
# 前端构建（输出 dist/，随后通过 go:embed 嵌入服务端二进制）
cd server/frontend && npm run build

# 运行测试
go test ./server/...
```

**注意**：`server/test/` 下的测试是手动的集成调试脚本而非单元测试 —— `TestUE` 会启动一个硬编码的 UE exe，`TestConnect` 依赖本地 8877 端口已有服务运行。不要盲目跑整个测试套件，按需单独运行：`go test ./server/test -run TestXxx`。

## 启动流程

`initialize.Server.Serve()`（[initialize/server.go](initialize/server.go)）是唯一入口，首次调用时依次：

1. `initServerDB()` / `initStorageDB()`：初始化两个 SQLite 数据库
2. `initMqttServer()`：启动内嵌 mochi-mqtt broker，并将 ws listener 桥接到 Gin 路由
3. 若 `modelLibrary.enabled`，扫描导入现有模型；生成 License 申请码并校验授权
4. 每次 Serve 前**清空**内存库中的 `Client` 和 `ServerInstance` 表（内存库重启即失效，清表保证状态一致）
5. `router.BuildRouter()` 构建 Gin 路由并 `ListenAndServe`（阻塞）

`Stop()` 关闭 listener 并断开所有客户端/管理员连接。

## 数据存储（两个独立的数据库）

| 全局变量 | 位置 | 表 | 说明 |
|---|---|---|---|
| `global.SERVER_DB` | 内存 SQLite（`file::memory:?cache=shared`） | `Client`、`ServerInstance` | 运行时状态，**进程重启即丢失** |
| `global.STORAGE_DB` | `constants.SAVE_DIR + "storage.db"`（`./thingue-launcher/`） | `CloudFile`、`CloudRes`、`ModelAsset` | 持久化数据 |

模型定义在 `common/model/`（不在此目录）。实例数据由客户端注册（`clientRegister`）写入 SERVER_DB，因此服务器重启后实例状态清空，等待客户端重新注册。

## 核心架构

### WebSocket 端点（`/ws/...`，见 [web/router/ws_router.go](web/router/ws_router.go)）

| 端点 | 连接方 | 生命周期管理 |
|---|---|---|
| `/ws/client` | 启动器客户端 | 连接即分配 client ID（写入 SERVER_DB），断开即删除；由 `ClientConnProvider` 按 client ID 维护连接，用于下发进程控制等消息 |
| `/ws/admin` | 管理后台前端 | 由 `AdminConnProvider` 维护；**状态变更后必须调用 `BroadcastUpdate()` 推送 "update" 消息**，前端收到后刷新列表 |
| `/ws/streamer/:id` | UE 实例（Pixel Streaming） | `id` 即实例 SID，必须在 SERVER_DB 中存在，否则 3 秒后关闭连接；对应 `StreamerConnector` |
| `/ws/player/:ticket` | 浏览器播放端 | 通过 ticket 换取 SID 后与 Streamer 配对，转发 SDP offer/answer/iceCandidate；对应 `PlayerConnector` |

### Pixel Streaming 信令流程（核心业务）

1. 播放端先请求 REST `POST /api/instance/ticketSelect`（`TicketService.TicketSelect2`）获取一次性 ticket（10 秒有效期，`gcache` 缓存 ticket→SID）。分配策略：请求按 `shared` 三态（nil/true=共享、false=独占）进类型池；**指名直选**（请求带 `sid` 或 `name`，含管理端实例列表点实例名跳转的 `player.html?sid=` 与 `getTicketById`）不参与分配策略——不做类型池、白名单准入与容量判据，只保留 ready 过滤，走 `ReserveDirect` 出票（预留标记 `direct`，配对时的独占容量兜底同样放行）；携带 sceneId 走四级优先级（已加载目标场景 → 空闲 → 连接数为 0 → 共享需 `EnableSharedInstance`）；无 sceneId 严格按客户端/实例顺序两段式分配（白名单命中段 → 无白名单段）。每步容量判断+预留（`Reserve`，独占恒 1、共享按 `MaxPlayerCount`，-1 不限）在 `resMu` 临界区内原子完成，ticket 在配对时 `Consume`（一次性）。被踢 IP 写入拒绝名单（`DenyService`，TTL 默认 60s），ticketSelect/ws 升级/配对三层拦截，断开码 4000，被拒 403
2. 播放端连接 `/ws/player/:ticket`，`SdpService.ConnectStreamer` 将 ticket 解析为 SID 并拿到 `StreamerConnector`；若实例未运行但开了 `AutoControl`，会先下发 START 并轮询等待 Streamer 连接
3. Player 发 `offer`/`subscribe` 后与 Streamer 配对（`OnPlayerPaired`），之后双向转发 `offer`/`answer`/`iceCandidate`
4. 玩家数变化时更新实例的 `PlayerCount` 并广播给管理端；开启 `EnableRenderControl` 时，无玩家会自动发送 rendering=false 指令关闭渲染
5. 玩家全部离开且实例开启自动启停（`AutoControl` + `StopDelay`）时，`AutoStopTimer` 倒计时后自动向客户端下发 STOP

**Pak/场景控制**：`PakControl`（load/unload）通过 `StreamerConnector.SendCommand` 向 UE 发 `command` 消息（`LoadBundle`/`unload`/`rendering`，定义在 `common/message/sdp_server.go`）。`CurrentPak` 记录实例当前加载的场景，Streamer 重连（`nodeRestarted`）后会按 `restartingMap` 中的重启标记自动重新加载。

### 分层与调用约定

```
web/handler/    → Gin 处理器，只做参数绑定和响应封装，不写业务逻辑
  ├─ rest/      REST API（instance/sync/model 三组）
  ├─ ws/        WebSocket 处理器（消息分发的 if-else 循环在 handler 层）
  └─ mqtt/      mochi-mqtt 的 listeners.Listener 实现（ws 桥接）
web/router/     → 路由组装，每个 router 结构体实现 BuildRouter()
core/service/   → 业务逻辑（单例，包级 var）
core/provider/  → 连接注册表与消息发送（单例，包级 var）
```

- **没有依赖注入**：handler 通过 [core/enter.go](core/enter.go) 的别名（`core.InstanceService`、`core.SdpService` 等）访问服务单例
- REST 响应必须用 `common/response` 的封装（`Ok`/`OkWithData`/`OkWithDetailed`/`FailWithMessage`）
- 所有路由挂在 `AppConfig.LocalServer.ContentPath` 前缀下（[web/router/base_router.go](web/router/base_router.go) 的 `BuildRouter` 是路由总入口），前端构建时用 `--base=./` 配合相对路径

### MQTT

内嵌 mochi-mqtt broker（`global.MQTT_SERVER`），WS 监听器通过 Gin 路由 `/mqtt` 桥接（见 [web/handler/mqtt/ws_handler.go](web/handler/mqtt/ws_handler.go)，`Init`/`Serve`/`Close` 被注释掉以复用 Gin 的 http.Server）。REST 端点 `POST /api/mqtt/publishPayload|publishText` 用于发布消息。

### 管理前端（server/frontend/）

- Vue 3 + Quasar + Vite + Monaco，构建输出 `dist/` 由 [server.go](server.go) 的 `//go:embed all:frontend/dist` 嵌入
- 两个入口页：`index.html`（管理后台）和 `player.html`（播放页）
- 静态文件通过 `/static/*` 路由从 embed FS 提供；`--staticDir` 参数可切换为外部静态目录（`UseExternalStatic`）；`/storage` 直接映射云资源文件目录

### License

[core/service/license_service.go](core/service/license_service.go)：基于机器指纹的授权校验，激活文件默认 `./thingue-launcher/thingue.lic`（可用 `THINGUE_LICENSE_PATH` 环境变量覆盖）。`ticketSelect` 在返回 ticket 前会校验 License 有效性，未授权时在响应中带出申请码。

## 注意事项

- **状态变更必须广播**：任何修改 SERVER_DB 中实例/客户端状态的操作后要调 `AdminConnProvider.BroadcastUpdate()`，否则管理端不会刷新
- **并发**：provider 中的 map（`ConnMap`、`idStreamerMap`、`idPlayerMap`）没有加锁，`instanceService` 用 `updateLock` 串行化实例更新——新增对 map 的并发读写时要注意
- **重启语义**：SERVER_DB 是内存库，任何"持久化"需求应放到 STORAGE_DB 或 `common/model` 之外的存储
- **新 REST 接口的步骤**：`web/handler/rest` 加处理器 → 对应 `*_router.go` 注册路由 → 前端 `api.js` 封装（如涉及管理端 UI）
- **模型库**：路由仅在 `AppConfig.ModelLibrary.Enabled` 时注册（[web/router/base_router.go](web/router/base_router.go)）；上传格式要求 multipart 包含 `metadata.json` + `asset.pak/utoc/ucas` + 两张缩略图
- **测试服务**：`ticket == "test"` 会连接 SID 为 `test` 的测试 Streamer（`SdpService.ConnectStreamer`）
