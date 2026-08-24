# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

本文件针对 `client/` 目录（ThingUE 启动器 GUI 客户端），是仓库根目录 CLAUDE.md 的补充，根目录中的整体架构、构建命令、消息协议说明依然适用。与 [server/CLAUDE.md](../server/CLAUDE.md) 配合阅读。

## 目录定位

`client/` 是 Wails v2 桌面应用本体，负责：

- **UE 实例管理**：实例的增删改查、启动/停止（`service/instance/`）
- **与信令服务器通信**：WebSocket 长连接 + REST HTTP 调用（`service/server/`、`service/instance/*_request.go`）
- **云资源同步**：按文件 hash 差量上传/下载实例包文件（`service/instance/sync_manager.go`）
- **GUI 前端**：Vue 3 + Quasar + Monaco，构建产物 `frontend/dist/` 通过 `go:embed` 打进二进制

**重要**：客户端内嵌整个服务端 —— [api/server_api.go](api/server_api.go) 直接 import `thingue-launcher/server/initialize`，通过 `initialize.Server.Start()/Stop()` 在 GUI 内托管本地信令服务（LocalServer）。`api/system_api.go` 也引用 `server/core/service` 的 `LicenseService`。修改服务端代码时注意客户端会复用。

## 启动流程

1. 根目录 `main.go`：`client.Init()` → `server.Init()` → `client.RunApp()`
2. [client.go](client.go)：`Init()` 创建 `constants.SAVE_DIR`（`./thingue-launcher/`）、`provider.InitConfigFromFile()` 加载配置、`initialize.InitGorm()` 打开 SQLite；`RunApp()` 调用 [initialize/app_initialize.go](initialize/app_initialize.go) 的 `InitApp(assets)` 启动 Wails 窗口
3. Wails `OnStartup` 中依次调用 `api.InstanceApi/ServerApi/SystemApi` 的 `Init(ctx)`，各自启动监听协程；App 退出时 `RunnerManager.CloseAllRunner()` 关闭所有运行中的实例

## 构建与测试

```bash
# 前端开发（vite dev server，端口 7789；wails dev 会自动拉起）
cd client/frontend && npm run dev
# 前端构建（输出 dist/，随后通过 go:embed 嵌入，改完前端后应用构建前必须执行）
cd client/frontend && npm run build

# 运行测试
go test ./client/...
```

**注意**：`client/` 下唯一的测试文件 [service/server/conn_manager_test.go](service/server/conn_manager_test.go) 是手动调试脚本而非单元测试 —— `TestName` 会读取配置中的 `ServerURL` 实际发起连接并阻塞数秒。按需单独运行 `go test ./client/service/server -run TestName`，不要盲目跑整个测试套件。

完整 GUI 应用构建见根目录 `build_window_amd64.bat` / `build_linux_amd64.sh`（`wails build`，会先执行 `frontend:install` + `frontend:build`）。

## 分层架构

```
api/                → Wails 绑定层：instanceApi / serverApi / systemApi 三个单例（包级 var）
  ↓ 委托
service/enter.go    → 服务单例别名（ServerConnManager、RunnerManager、InstanceManager、
                      SyncManager、RunnerRestartTaskManager），无依赖注入
service/server/     → conn_manager.go（WS 连接）、msg_receiver.go（消息分发）
service/instance/   → runner 生命周期、GORM 持久化、HTTP 请求、云同步
global/global.go    → 包级全局：APP_DB、APP_VP、CLIENT_ID
initialize/         → Wails App 装配、GORM、Wails 日志适配器（ZapLogger）
frontend/           → Vue 3 前端 + wailsjs 生成代码
```

### api/ —— Wails 绑定

- 每个 api 结构体导出方法自动绑定到前端，前端通过 `@wails/go/api/<name>`（alias 到 `frontend/wailsjs/`，见 [vite.config.js](frontend/vite.config.js)）以 camelCase 调用
- `Init(ctx)` 保存 context 并启动事件转发协程：Go 侧的 channel（`RunnerStatusUpdateChanel` 等）→ `runtime.EventsEmit(ctx, 事件名)` → 前端 `window.runtime.EventsOn("事件名")` 接收。事件名定义在 `common/constants/event.go`（`runner_unexpected_exit`、`runner_status_update`、`remote_server_conn_update`、`local_server_close`）
- `frontend/wailsjs/` 由 wails 自动生成，**不要手改**；修改 api 方法签名后运行 `wails dev`/`wails build` 重新生成

### service/server/ —— 服务器连接

[conn_manager.go](service/server/conn_manager.go)：`ConnManager` 单例维护与服务器的 WebSocket 连接（`/ws/client`）：
- `SetServerAddr` 持久化到 `provider.AppConfig.ServerURL`；断线后 2 秒重试，`StartConnectTask` 定时重连
- 连接成功后：设置 `instance.BaseRequest.SetBaseUrl()`（所有 HTTP 请求依赖此 base URL，未连接时请求直接报"服务未连接"）、启动 40 秒心跳 ping、进入读循环
- 连接断开（读失败或被关闭）→ 清空 BaseUrl → 通过 `ServerConnUpdateChanel` 通知前端

[msg_receiver.go](service/server/msg_receiver.go)：`MsgReceive` 按 `msg.Type` 分发，当前处理五种消息：`ServerConnectCallback`（服务器分配 `global.CLIENT_ID` 后调 `ClientService.RegisterClient` 上报设备信息和实例列表）、`ServerProcessControl`（START/STOP 命令）、`ServerStreamerConnectedUpdate`、`ServerCollectClientLogs`、`SyncUpdate`。

### service/instance/ —— 实例管理

- [instance_manager.go](service/instance/instance_manager.go)：GORM 持久化（`global.APP_DB`，SQLite 位于 `./thingue-launcher/config.db`），表为 `ClientInstance` + `RemoteServer`（[gorm_initialize.go](initialize/gorm_initialize.go) 中 AutoMigrate）。`SaveConfig` 会同步把变更 copy 到内存中的 Runner
- [runner_manager.go](service/instance/runner_manager.go)：`RunnerManager` 维护 `CID → *Runner` 映射。`Init()` 时先在工作目录发现内置实例（`ThingUE.exe`/`ThingUE.sh`，`IsInternal=true`，自动创建），再从 DB 实例化其余 Runner
- [runner.go](service/instance/runner.go)：`Runner.Start()` 先向服务器请求 SID（`getInstanceSid`），再以 `-PixelStreamingURL=ws://<server>/ws/streamer/<SID>` 和 `LOG=<实例名>.log` 参数启动进程；`os_cmd.Capture` 捕获进程组（Windows Job Object / Linux setpgid）以便整树终止，失败时回退 taskkill/kill。**异常退出**时（ExitSignalChannel 满 → 状态码 -1）若开启 `FaultRecover` 且 faultCount < 3 会自动重启
- **StateCode 语义**：0=停止、1=运行中、-1=异常退出（前端 [utils.js](frontend/src/utils.js) 的 `RunnerStateCodeToString`）。状态变化通过 `RunnerStatusUpdateChanel`/`RunnerUnexpectedExitChanel` 推送前端，并 `ClientService.SendProcessState` 上报服务器
- [client_request.go](service/instance/client_request.go) / [sync_request.go](service/instance/sync_request.go)：嵌入 `baseRequest` 的 REST 客户端。`ClientService`：getInstanceSid、clientRegister（注册成功后自动启动 AutoStart 实例）、updateProcessState、setRestarting、clearPak、上传日志压缩包；`SyncRequest`：云资源文件列表/上传/删除
- [sync_manager.go](service/instance/sync_manager.go)：云资源同步。上传：本地文件按 hash 与云端 diff，新增/修改走上传、缺失走删除；更新：反向 diff 下载。由服务器 `SyncUpdate` 消息或前端手动触发
- [runner_restart_task_manager.go](service/instance/runner_restart_task_manager.go)：robfig/cron 定时重启任务，cron 表达式来自 `SystemSettings.RestartTaskCron`
- [device_info.go](service/instance/device_info.go)：ghw 采集 CPU/内存/显卡/网卡等硬件信息，随注册上报

### frontend/ —— Vue 3 前端

- 入口 [main.js](frontend/src/main.js) → [App.vue](frontend/src/App.vue)，三个竖向 Tab：unreal（实例列表/配置）、server（本地/远程服务器）、settings
- 组件：`components/unreal/`（实例管理，[settingsEditor.js](frontend/src/components/unreal/settingsEditor.js) 封装 Monaco 编辑器）、`components/server/`（LocalServerPanel 控制内嵌服务器，RemoteServerPanel 管理远程服务器）、[Setting.vue](frontend/src/components/Setting.vue)
- 调用后端统一 `@wails/go/api/...` 生成的 Promise 包装；接收事件统一 `window.runtime.EventsOn`

## 注意事项

- **实例变更必须重连**：创建/保存/删除实例后都要调 `ServerConnManager.Reconnect()`（关闭旧连接触发重连重注册），否则服务器端的实例信息不会更新
- **HTTP 请求依赖连接状态**：`BaseRequest.BaseUrl` 只在 WS 连接成功时设置，所有走 `baseRequest` 的调用要容忍"服务未连接"错误
- **内置实例不可删**：`IsInternal` 实例 `DeleteRunner` 直接拒绝；实例运行中也不可删除或改配置
- **配置文件**：`provider.AppConfig`（viper）运行时修改后需显式 `provider.WriteConfigToFile()` 持久化到 `./thingue-launcher/config.yaml`；`ServerURL`、`SystemSettings`、`LocalServer`、`PeerConnectionOptions` 均通过此机制
- **进程终止**：跨平台进程树管理在 [os_cmd/](service/instance/os_cmd/)（build tags 区分 Windows/Linux），Windows 用 Job Object，Linux 用 setpgid+kill
- **新增 API 方法**：在 `client/api/` 相应结构体上写导出方法即自动绑定；需要注册事件时在 `common/constants/event.go` 加常量、`Init` 中加转发协程、前端 `EventsOn` 接收
