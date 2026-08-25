# ThingUE 启动器 实例分配改造方案

> **需求来源**：[thingue.launcher修改需求.md](thingue.launcher修改需求.md)（5 节需求 + 1 个问题）
> **涉及仓库**：`thingue-launcher`（本仓库，服务端 + 管理端前端）、`D:\unreal\projects\uino\thinguelib`（拉流 SDK，独立仓库）
> **文档状态**：设计稿（本文档只描述方案，不包含已完成的代码实现）

---

## 一、背景与目标

当前系统支持播放端（拉流 SDK / 播放页）通过 `POST /api/instance/ticketSelect` 申请实例后连接拉流，但存在以下缺口：

1. 管理端看不到每个实例当前连接了哪些客户端 IP，也无法主动断开某个 IP 或全部断开；
2. 实例没有"白名单"概念，任何 IP 都可能被分配到任意实例；
3. 实例没有"独占/共享"类型与"连接数上限"概念；
4. 客户端请求无法明确声明"本次要独占还是共享实例"；
5. 分配策略不区分实例类型，无法按白名单与容量做确定性分配；
6. 服务端断开客户端后，客户端会自动重连，导致断开操作失效（需求文档问题 1）。

**目标**：按需求文档 5 节内容完成改造设计，并给出问题 1 的结论与规避方案，方案覆盖并发竞态、数据持久化、兼容性与各类边界场景（"滴水不漏"）。

---

## 二、现状分析

### 2.1 现有链路

```
thinguelib / 播放页
   │  POST /api/instance/ticketSelect   body: {name, sceneId, shared, playerCount, labelSelector}
   ▼
TicketService.TicketSelect2  （server/core/service/ticket_service.go:156-362）
   │  License 校验 → DB 过滤 → 四级优先级选实例 → gcache 写 ticket（10 秒 TTL）
   ▼
ws://<server>/ws/player/<ticket>
   │  PlayerWebSocketHandler 升级 → SdpService.ConnectStreamer（ticket→SID 配对 StreamerConnector）
   ▼
offer/subscribe → OnPlayerPaired → StreamerConnector.PlayerConnectors append
   │  UpdatePlayers → ServerInstance.PlayerIds / PlayerCount → BroadcastUpdate → 管理端刷新
   ▼
管理端 server/frontend（InstanceList.vue 子表显示 playerIds.length 为"连接数"）
```

关键文件与行号：

| 环节 | 位置 |
|---|---|
| 拉流 SDK 请求构造 | thinguelib `src/ThingUE.js` L107-130（构造参数，L115 `shared` 默认 true）、L514-539（`urlBuilder` 发 ticketSelect） |
| 请求结构 | [common/request/request.go:21-29](../common/request/request.go#L21-L29) `SelectorCond`（已有 `Shared bool json:"shared"`） |
| 分配核心 | [server/core/service/ticket_service.go:156-362](../server/core/service/ticket_service.go#L156-L362) `TicketSelect2` |
| ticket 缓存 | 同文件 L19-25（gcache LRU），`GetSidByTicket` L41-48（**不删除 ticket**） |
| 实例模型 | [common/model/instance_server.go:12-44](../common/model/instance_server.go#L12-L44) `ServerInstance` |
| 占用判定（派生） | 同文件 L72-80 `IsIdle`、L82-87 `IsAccessing` |
| 配对与计数 | [server/core/service/sdp_service.go](../server/core/service/sdp_service.go) `OnPlayerPaired` L121-130、`OnPlayerDisConnect` L132-146、`ConnectStreamer` L64-112 |
| 玩家连接器 | [server/core/provider/player_connector.go:13-19](../server/core/provider/player_connector.go#L13-L19) `PlayerConnector`（无 IP 字段）；`Kick` L104-107（CloseMessage 4000 "kicked"） |
| 玩家 WS 入口 | [server/web/handler/ws/player_ws_handler.go:12-66](../server/web/handler/ws/player_ws_handler.go#L12-L66) |
| 管理端列表 | server/frontend/src/components/InstanceList.vue（子表 L22-26 已有"连接数"列） |
| 管理端设置面板 | server/frontend/src/components/InstanceInfo.vue（**只读，无保存逻辑**） |

### 2.2 关键缺口（改造前必须认清的事实）

1. **服务端从未采集客户端 IP**：全仓库 `RemoteAddr / ClientIP / X-Forwarded-For` 零命中。`ServerInstance.PlayerIds` 只存 playerId，不含 IP。
2. **无"已分配/占用"显式状态**：由 `StateCode` + `PlayerCount` 派生判断；无类型、白名单、连接数上限字段。
3. **`shared` 参数语义与需求不符**：thinguelib 已传 `shared`（默认 true），但服务端只在"无空闲实例时的第 4 级兜底"使用它（ticket_service.go L288：`EnableSharedInstance && enableShared`），并非"独占/共享"类型请求。
4. **SID 不持久化**：[common/model/instance_client.go:13](../common/model/instance_client.go#L13) `SID string json:"sid" gorm:"-"`，客户端重启后 SID 重新生成——任何按 SID 的服务端持久化配置都会失效，必须一并解决。
5. **SERVER_DB 是内存库**：`file::memory:?cache=shared`，每次 Serve 前清空 `Client`/`ServerInstance` 表（见 server/CLAUDE.md"重启语义"）；持久化必须放 STORAGE_DB（`storage.db`）。
6. **无"断开客户端"类消息/接口**：消息类型仅 5 种（`common/message/types/types.go` L3-9）；踢玩家仅 `Kick()`（close 4000）与按 UserData 匹配的 `kickPlayerUser`。
7. **客户端无条件自动重连**（问题 1 的事实基础，见第九章）。
8. **既有漏洞**：`GetSidByTicket` 不删除 ticket → 同一 ticket 在 10 秒内可被多个玩家复用（改造时一并堵住）。

---

## 三、总体设计（决策摘要）

| 决策点 | 结论 |
|---|---|
| 配置持久化 | `ClientInstance.SID` 持久化 + STORAGE_DB 新增 `InstanceSettings` 表（按 SID），register 时合并（论证见 4.4） |
| 实例类型 | `ServerInstance.InstanceType`：0=共享（默认，兼容现状）、1=独占；`EnableSharedInstance` 保留原语义（仅作为 SceneId 第 4 级"共享加载"开关） |
| 请求参数 | `SelectorCond.Shared` 由 `bool` 改 `*bool` 三态：nil=未携带（旧播放页）、true=共享、false=独占 |
| IP 采集 | 全部由服务端 `gin c.ClientIP()` 采集；客户端 body 不可自报；`SetTrustedProxies(nil)` 防 X-Forwarded-For 伪造 |
| 占用判定 | 独占：`PlayerCount + 预留数 >= 1`；共享：`MaxPlayerCount <= 0` 永不判满，否则 `PlayerCount + 预留数 >= 上限` |
| 竞态防护 | ticketService 新增 `resMu + reservations + sidReserved`，预留与 ticket 同 10s TTL，配对时消费并二次校验 |
| 问题 1 规避 | 三层：服务端"被踢 IP 拒绝名单"（TTL 默认 60s 可配置）+ thinguelib 收到 close 4000 停重连 + 配对二次校验兜底 |
| IP 列数据源 | `ServerInstance.PlayerIps` 镜像字段（与 PlayerIds 同快照生成、随 BroadcastUpdate 一次下发） |
| "未分配"状态 | 派生状态（独占实例 `PlayerCount==0` 即未分配），前端展示，服务端不新增字段 |

---

## 四、数据模型与持久化（需求 2、3）

### 4.1 `ServerInstance` 新增字段

[common/model/instance_server.go](../common/model/instance_server.go)：

```go
type ServerInstance struct {
    // ...现有字段全部不变...
    EnableSharedInstance bool `json:"enableSharedInstance"` // 保留：SceneId 第4级"共享加载"开关
    // ---- 新增 ----
    InstanceType   int8        `json:"instanceType" gorm:"default:0"`    // 0=共享(默认) 1=独占
    Whitelist      StringSlice `json:"whitelist"`                        // 白名单 IP；空=不过滤
    MaxPlayerCount int         `json:"maxPlayerCount" gorm:"default:-1"` // 共享上限：-1=不限(默认)；独占忽略(固定1)
    PlayerIps      StringSlice `json:"playerIps"`                        // 已连接客户端 IP 镜像（权威数据在 PlayerConnectors）
}
```

要点：

- **默认值即现状**：存量实例 `InstanceType=0`（共享）、`MaxPlayerCount=-1`（不限）→ 未配置过类型/上限的部署行为与改造前一致；管理员按需把实例切换为独占。
- **`EnableSharedInstance` 与 `InstanceType` 正交**：前者继续只控制"SceneId 分配第 4 级是否允许把该实例当共享场景加载目标"；"是否共享型实例"一律看 `InstanceType`。
- **独占实例的 `MaxPlayerCount` 被忽略**，恒按 1 计算；管理端对独占实例禁用上限输入框。
- SERVER_DB 为内存库、每次 Serve 前清空重建，AutoMigrate 自动加列，**不存在存量数据迁移问题**。

### 4.2 `ClientInstance.SID` 持久化（配置持久化的前提）

[common/model/instance_client.go:13](../common/model/instance_client.go#L13)：

```go
SID string `json:"sid" gorm:"-"`   // 改为：
SID string `json:"sid"`            // 去掉 gorm:"-"，持久化到客户端 config.db
```

- 客户端 `client/initialize/gorm_initialize.go` 已 AutoMigrate `ClientInstance`，SQLite 自动加列。
- SID 由此成为实例跨启动器重启、跨 server 重启的稳定标识，是设置持久化的键。
- 兜底：`ClientRegister`（[client_service.go:38-41](../server/core/service/client_service.go#L38-L41)）与 `ToServerInstance`（[instance_client.go:36-45](../common/model/instance_client.go#L36-L45)）中保留 `SID==""` 时生成 UUID 的既有逻辑（老 config.db 首次启动自动补 SID）。

### 4.3 STORAGE_DB 新增 `InstanceSettings` 表

新文件 `common/model/instance_settings.go`：

```go
type InstanceSettings struct {
    ID             uint        `json:"id" gorm:"primarykey"`
    SID            string      `json:"sid" gorm:"uniqueIndex;size:64"` // 关联 ClientInstance.SID
    InstanceType   int8        `json:"instanceType" gorm:"default:0"`  // 0=共享 1=独占
    Whitelist      StringSlice `json:"whitelist"`                      // 空=不过滤
    MaxPlayerCount int         `json:"maxPlayerCount" gorm:"default:-1"`
    LastSeenAt     time.Time   `json:"lastSeenAt"`                     // 最近一次 register 合并时间，孤儿行清理依据
    UpdatedAt      time.Time   `json:"updatedAt"`
    CreatedAt      time.Time   `json:"createdAt"`
}
```

`server/initialize/gorm.go` 的 `initStorageDB()` AutoMigrate 列表追加 `&model.InstanceSettings{}`。

### 4.4 register 时合并

[server/core/service/client_service.go](../server/core/service/client_service.go) `ClientRegister`（L30-48）循环内：

```go
for _, instance := range registerInfo.Instances {
    var serverInstance = &model.ServerInstance{}
    mapstructure.Decode(instance, serverInstance)
    if serverInstance.SID == "" {
        serverInstance.SID = uuid.New().String()
    }

    // 新增：SID 冲突检测（克隆 config.db 场景），冲突方换新 SID（获得默认设置）
    var cnt int64
    global.SERVER_DB.Model(&model.ServerInstance{}).
        Where("s_id = ? AND (client_id <> ? OR c_id <> ?)", serverInstance.SID,
              client.ID, serverInstance.CID).Count(&cnt)
    if cnt > 0 {
        serverInstance.SID = uuid.New().String()
    }

    // 新增：合并持久化设置
    var settings model.InstanceSettings
    if err := global.STORAGE_DB.Where("s_id = ?", serverInstance.SID).
        First(&settings).Error; err == nil {
        serverInstance.InstanceType = settings.InstanceType
        serverInstance.Whitelist = settings.Whitelist
        serverInstance.MaxPlayerCount = settings.MaxPlayerCount
        settings.LastSeenAt = time.Now()
        global.STORAGE_DB.Save(&settings)
    }
    serverInstances = append(serverInstances, serverInstance)
}
```

孤儿行清理：Serve 启动时删除 `LastSeenAt` 距今超过 30 天的 `InstanceSettings` 行（客户端删除实例/重装后自然回收）。

### 4.5 方案选优：STORAGE_DB 持久化（A） vs 客户端为配置源 + WS 回写（B）

**选 A**，理由：

1. **写路径单一**：管理端是白名单/类型/上限的唯一编辑入口，直接写 server 即可。方案 B 需要"管理端 → server → 新 WS 消息 → 客户端写 config.db → 客户端重连重注册"完整回写链，链路长、失败点多（客户端离线时消息排队/丢弃、写盘无反馈、与用户在客户端本地的编辑互相覆盖）。
2. **离线语义一致**：客户端离线时 `ClientOffline` 已删除其 ServerInstance 行（[client_service.go:60-64](../server/core/service/client_service.go#L60-L64)），管理端 UI 本来就不显示该实例——A 的"离线不可编辑"与现状一致；B 反而制造"写回离线客户端"的额外复杂度。
3. **复用已有模式**：STORAGE_DB 已有 CloudFile/CloudRes/ModelAsset 持久化先例；SERVER_DB 重启清空、等客户端重连重注册的语义早已存在（客户端 2 秒定时重连自动恢复，见 [conn_manager.go](../client/service/server/conn_manager.go) L114-132）。
4. **顺带修复**：SID 持久化消除"每次注册 SID 都变"的隐患（ticket/设置无法稳定关联）。

**A 的丢失/不一致边界（明示）**：

| 场景 | 后果 | 处理策略 |
|---|---|---|
| 客户端 config.db 删除/重装 | SID 变化，旧设置成孤儿行 | `LastSeenAt` 超 30 天自动清理 |
| 克隆 config.db 双客户端同 SID | unique 冲突 | register 冲突检测，后注册方换新 SID（默认设置） |
| server 重启 | SERVER_DB 清空、占用归零 | 客户端 2s 重连重注册 → 合并恢复，无需人工干预 |
| 客户端离线期间 | 实例行被删，不可编辑 | 与现状一致，接受 |
| 多管理员并发改同一实例 | 单行 upsert last-write-wins | 接受，BroadcastUpdate 保证 UI 最终一致 |
| 设置写入与 register 同时发生 | 双库更新 | SERVER_DB 经 updateLock 串行；STORAGE_DB 单行 upsert，最终一致 |

---

## 五、客户端 IP 采集与信任边界（需求 1、2、5 的基础）

### 5.1 采集点与存放

**ticketSelect（REST）**：[server/web/handler/rest/instance_handler.go:141-150](../server/web/handler/rest/instance_handler.go#L141-L150) `TicketSelect`：

```go
var selectCond request.SelectorCond
_ = c.ShouldBindJSON(&selectCond)
selectCond.ClientIP = c.ClientIP()   // 服务端采集，覆盖一切客户端输入
```

[common/request/request.go:21-29](../common/request/request.go#L21-L29) `SelectorCond` 改造：

```go
type SelectorCond struct {
    SID               string `json:"sid"`
    Name              string `json:"name"`
    PlayerCount       *int   `json:"playerCount"`
    LabelSelector     string `json:"labelSelector"`
    StreamerConnected bool   `json:"streamerConnected"`
    SceneId           string `json:"sceneId"`
    Shared            *bool  `json:"shared"` // 三态：nil=未携带(旧请求)，true=共享，false=独占
    ClientIP          string `json:"-"`      // 服务端填充（gin c.ClientIP），禁止客户端绑定
}
```

**/ws/player upgrade**：[server/web/handler/ws/player_ws_handler.go:13-21](../server/web/handler/ws/player_ws_handler.go#L13-L21)：

```go
conn, err := WsUpgrader.Upgrade(c.Writer, c.Request, nil)
...
player := provider.SdpConnProvider.NewPlayer(conn)
player.IP = c.ClientIP()    // PlayerConnector 新增字段 IP string
```

### 5.2 信任边界（X-Forwarded-For 伪造）

本仓库使用 **gin v1.9.1**（go.mod L11），该版本默认 `TrustedProxies=["0.0.0.0/0","::/0"]`——**默认信任任意来源的 X-Forwarded-For 头**，客户端可直接伪造白名单 IP。处理：

1. `server/initialize/server.go` 路由构建处执行 `router.SetTrustedProxies(nil)`——直连部署下 `ClientIP()` 回落到 `RemoteAddr`，伪造头被忽略；
2. [common/provider/config_provider.go:24-30](../common/provider/config_provider.go#L24-L30) 的 `LocalServer` 新增 `TrustedProxies []string yaml:"trustedProxies"`，反代/NAT 部署时显式配置代理网段；
3. 客户端 body 自报的任何 IP 字段一律 `json:"-"` 或忽略。

---

## 六、分配策略（需求 4、5）

### 6.1 通用定义

- **"按顺序"** = 实例配置顺序：候选查询统一追加 `Order("c_id asc")`（`c_id` 是启动器客户端本地自增主键，即实例在配置中的顺序；管理端列表同序展示）。
- **白名单语义**：`len(Whitelist)==0` 不过滤；非空时仅 `Contains(clientIP)` 命中（IP 用 `net.ParseIP` 规范化后比较，兼容 IPv4/IPv6）；`ClientIP` 为空不命中任何白名单。
- **占用/满判据**（均含预留数，见第七章）：
  - 独占占用：`PlayerCount + reservedCount(sid) >= 1`
  - 共享满：`MaxPlayerCount <= 0` 永不判满；否则 `PlayerCount + reservedCount(sid) >= MaxPlayerCount`
- **类型池不交叉回退**：独占请求只在 `InstanceType==1` 中选；共享请求只在 `InstanceType==0` 中选；池空直接报错。

### 6.2 请求类型三态（需求 4）

| `Shared` 取值 | 来源 | 处理 |
|---|---|---|
| `nil`（未携带） | 旧播放页（server 自带 player_old 页面等） | 按**共享请求**处理 + 沿用**旧选择排序**（见 6.5），保证不回归 |
| `true` | thinguelib 默认、新版播放页 | 共享池 + 5.2 策略 |
| `false` | thinguelib `shared:false` | 独占池 + 5.1 策略 |

> 设计说明：`Shared` 必须是 `*bool` 而非 `bool`——旧播放页不传该字段，`bool` 会误判为独占请求，在"全部实例为共享型"的部署下旧页将全部失败。三态同时保证：旧请求依然受类型池（仅共享型）、白名单、容量约束，**无法借旧页面绕过白名单与独占限制**。

### 6.3 两段式选择（需求 5.1 / 5.2 的核心）

对候选实例做两段遍历（需求原文语义）：

```
第一段：白名单命中段 —— len(Whitelist)>0 且 Whitelist 包含 ClientIP，且容量满足（独占未占用 / 共享未满）
第二段：无白名单段   —— len(Whitelist)==0，且容量满足
（配置了白名单但未命中的实例，两段都不参与）
```

- 段间顺序固定：第一段全部落空后才进第二段（需求 5.1/5.2 原文即两段式，不是混合排序）。
- 段内顺序：见 6.5。
- 两段全部落空 → 独占请求报"独占实例已全部被占用"；共享请求报"共享实例连接数已满"；池空时分别报"没有可用的独占实例"/"没有可用的共享实例"。
- **需求解读（宽松回落语义）**：白名单命中但该实例已占用/已满 → 跳过、可回落到无白名单实例（与 5.1 独占的逐实例条件一致，避免白名单用户被整体拒绝）。若产品上需要"命中白名单即不再回落"的严格语义，可后续在 `updateInstanceSettings` 增加开关，本期不实现。

### 6.4 SID 直选与既有分支的处置

- **SID 直选**（ticket_service.go L214-225）：显式指定 SID 跳过选择策略，但仍做容量校验与预留——独占已占用报"该实例已被独占占用"、共享已满报"该实例连接数已满"。
- **License 校验**（L158-177）、**DB 过滤**（L178-195）、**ready 过滤**（L203-212：`StateCode==1 || AutoControl==true`）全部保留不变。
- **labelSelector**（L307-334）：保留，作为候选过滤器（匹配后再进入两段式）。
- **LoadBundle 时机不变**：命中级 3/级 4（见 6.5）后、出票前调用（[ticket_service.go:364-371](../server/core/service/ticket_service.go#L364-L371)），异步命令，UE 加载失败不影响 ticket，与现状一致。

### 6.5 段内排序：需求"按顺序"与现有场景优先级的折中

| 请求携带 | 段内排序 |
|---|---|
| 无 sceneId、无 labelSelector | **严格 c_id 升序**（需求原文精确满足；独占请求的典型场景） |
| 有 labelSelector | 标签匹配后 c_id 升序（保留标签过滤，去掉旧"最小 PlayerCount"负载均衡——需求 5.2 要求按顺序找第一个未满，负载均衡与新语义冲突） |
| 有 sceneId | 保留现有场景四级优先级（级内 c_id 升序）：①已加载目标场景（CurrentPak==sceneId && StateCode==1）→ ②空闲（IsIdle）→ ③连接数为 0 的预备实例（!IsAccessing，命中后 LoadBundle）→ ④共享型且 EnableSharedInstance 且未满（LoadBundle） |

> 设计说明：thinguelib 始终携带 sceneId，场景四级优先级是其场景加载行为的既有语义（优先复用已加载场景、避免不必要的场景切换）；不携带 sceneId 的请求（独占为主）严格按需求"按顺序"执行。若产品希望携带 sceneId 时也完全按 c_id 顺序，只需删除 6.5 第三行的四级排序，改动点集中在一处。

### 6.6 重构后主流程伪码

```go
func (s *ticketService) TicketSelect2(cond request.SelectorCond) (response.InstanceTicket, error) {
    // 1) License 校验（现状保留）
    // 2) 被踢拒绝名单检查（第九章）
    if DenyService.IsDenied(cond.ClientIP) {
        return ticket, errors.New(ErrKickedDenied)   // handler 转 403
    }
    // 3) DB 过滤（现状）+ Order("c_id asc")；SID 直选提前分支（带容量校验+预留）
    // 4) ready 过滤（StateCode==1 || AutoControl，现状）
    // 5) 类型池过滤（三态：nil 与 true 都进共享池；false 进独占池）
    //    池空 → 报"没有可用的共享实例" / "没有可用的独占实例"
    // 6) 候选排序（6.5 规则；nil 请求沿用旧四级/空闲优先排序）
    // 7) 两段式遍历（6.3）：
    for _, pass := range []bool{true, false} {        // true=白名单命中段, false=无白名单段
        for _, inst := range candidates {
            if pass != whitelistHit(inst, cond.ClientIP) { continue }
            if s.isFull(inst, isSharedReq) { continue }      // 容量判据（含预留）
            // 8) 原子预留（第七章）；失败(竞态)则 continue 下一个
            if err := s.Reserve(inst.SID, cond.ClientIP); err != nil { continue }
            // 9) 副作用（现状保留）：SceneId 级2/3 记 SceneId+UpdateInstance；级3/4 LoadBundle
            // 10) 出票：gcache 10s + SetInstanceInfo
            return ticket, nil
        }
    }
    // 11) 两段全部落空
    return ticket, errors.New(map[bool]string{
        true: "共享实例连接数已满", false: "独占实例已全部被占用"}[isSharedReq])
}

func whitelistHit(inst *model.ServerInstance, ip string) bool {
    if len(inst.Whitelist) == 0 { return false }   // 空白名单归第二段
    return contains(inst.Whitelist, ip)
}
```

---

## 七、并发与竞态防护

现有并发事实：`provider` 中的 map 无锁；`instanceService` 用 `updateLock` 串行化实例更新（server/CLAUDE.md"注意事项"）；`GetSidByTicket` 不删 ticket。改造必须堵住"两个玩家同时抢同一独占实例"与"ticket 10 秒 TTL 内的复用"。

### 7.1 ticketService 预留结构

[server/core/service/ticket_service.go](../server/core/service/ticket_service.go)：

```go
type ticketReservation struct {
    sid      string
    ip       string
    expireAt time.Time
    consumed bool
}

type ticketService struct {
    cache        gcache.Cache
    resMu        sync.Mutex                         // 新增：预留锁
    reservations map[string]*ticketReservation      // 新增：ticket → 预留
    sidReserved  map[string]uint                    // 新增：sid → 未消费预留数
}

// 惰性清理：每次 Reserve/Consume/计数前调用
func (s *ticketService) sweepLocked(now time.Time) {
    for t, r := range s.reservations {
        if now.After(r.expireAt) {
            delete(s.reservations, t)
            if s.sidReserved[r.sid] > 0 { s.sidReserved[r.sid]-- }
            s.cache.Remove(t)   // 同步清 ticket 缓存：杜绝"预留没了票还在"
        }
    }
}

// 原子"判断容量 + 预留计数"，返回 ticket
func (s *ticketService) Reserve(sid, ip string, shared bool) (string, error) {
    s.resMu.Lock(); defer s.resMu.Unlock()
    s.sweepLocked(time.Now())
    inst := InstanceService.GetInstanceBySid(sid)
    if !shared {
        if inst.PlayerCount+s.sidReserved[sid] >= 1 { return "", errors.New("实例已被独占占用") }
    } else if inst.MaxPlayerCount > 0 &&
        inst.PlayerCount+s.sidReserved[sid] >= uint(inst.MaxPlayerCount) {
        return "", errors.New("共享实例连接数已满")
    }
    ticket := uuid.New().String()
    s.reservations[ticket] = &ticketReservation{sid: sid, ip: ip,
        expireAt: time.Now().Add(10 * time.Second)}
    s.sidReserved[sid]++
    s.cache.SetWithExpire(ticket, sid, 10*time.Second)
    return ticket, nil
}

// 配对时消费：校验存在/未过期/未消费/归属 → 扣减
func (s *ticketService) Consume(ticket, sid string) error {
    s.resMu.Lock(); defer s.resMu.Unlock()
    s.sweepLocked(time.Now())
    r, ok := s.reservations[ticket]
    if !ok || r.consumed || r.sid != sid { return errors.New("ticket无效或过期") }
    r.consumed = true
    if s.sidReserved[sid] > 0 { s.sidReserved[sid]-- }
    delete(s.reservations, ticket)
    s.cache.Remove(ticket)     // 顺带修复既有漏洞：GetSidByTicket 不删 ticket
    return nil
}
```

要点：

- **预留与 ticket 同 10 秒 TTL**：sweep 时同步 `cache.Remove(ticket)`，保证"票无效则预留必然已失效"，并堵住既有"同 ticket 10 秒内可复用"漏洞。
- **两个玩家同时抢同一独占实例**：`Reserve` 的容量判断与计数在同一 `resMu` 临界区内完成，第二个请求必失败（换下一实例或报"已全部被占用"）。
- **锁序约定**（防死锁，写入代码注释）：`resMu` →（释放后）→ `streamer.playersMu` →（释放后）→ `instanceService.updateLock`，三个锁从不嵌套持有。

### 7.2 配对时二次校验

[server/core/service/sdp_service.go](../server/core/service/sdp_service.go)：

```go
// ConnectStreamer：ticket 校验阶段
sid, err := TicketService.GetSidByTicket(ticket)
if err != nil {
    player.SendCloseMsg(4001, "ticket无效或过期")
    return err
}
if DenyService.IsDenied(player.IP) {          // ticket 发出后被踢的窗口
    player.SendCloseMsg(4000, "kicked")
    return errors.New("已被断开")
}

// OnPlayerPaired：offer/subscribe 到达时（最终消费点）
func (m *sdpService) OnPlayerPaired(player *provider.PlayerConnector) error {
    if err := TicketService.Consume(player.Ticket, player.StreamerConnector.SID); err != nil {
        player.SendCloseMsg(4001, "ticket无效或过期")
        return err
    }
    if DenyService.IsDenied(player.IP) {      // 兜底复检
        player.SendCloseMsg(4000, "kicked")
        return errors.New("已被断开")
    }
    // 独占容量防御性兜底（预留已防，双保险）：
    // 在 playersMu 临界区内：独占实例已有玩家 → SendCloseMsg(4001) 并 return
    // 正常配对：append + SendPlayersCount + UpdatePlayers（现状逻辑）
}
```

注意：`PlayerConnector` 需新增 `Ticket string` 字段（在 `ConnectStreamer` 成功时记录），供 `Consume` 校验归属。`OnPlayerPaired` 在旧代码里每次 offer 都会调用，改造后需加 `paired` 幂等标记，避免重复 Consume。

### 7.3 provider 无锁 map 的加锁改造

1. **`StreamerConnector` 增加 `playersMu sync.Mutex`**（[streamer_connector.go](../server/core/provider/streamer_connector.go)）+ 方法 `AddPlayer(p)` / `RemovePlayer(p)` / `Players() []*PlayerConnector`（加锁快照）。替换全部裸访问点：`OnPlayerPaired` 的 append、`OnPlayerDisConnect`/`PlayerDisconnect` 的 remove、`SendPlayersCount`、`OnStreamerDisconnect`、`UpdatePlayers`（[instance_service.go](../server/core/service/instance_service.go) 改为用 `Players()` 快照）、`OnStreamerLoadBundleComplete`、`OnStreamerNodeRestarted` 的 `len(...)`。
2. **`sdpConnProvider` 增加 `mapLock sync.RWMutex`**（[sdp_conn_provider.go](../server/core/provider/sdp_conn_provider.go)）：保护 `idPlayerMap`/`idStreamerMap` 增删与遍历；新增 `GetPlayersByIp(ip string)` 用读锁遍历。
3. `instanceService.updateLock` 继续仅包裹 DB 写（`UpdatePlayers` 等），不扩展职责。

### 7.4 时序（独占实例两玩家竞争）

```
P1: ticketSelect → Reserve(锁内: PlayerCount0+reserved0<1 ✓, reserved→1) → ticket1
P2: ticketSelect → Reserve(锁内: 0+1>=1 ✗) → 换下一实例或报"独占实例已全部被占用"
P1: ws connect → ConnectStreamer(票有效) → offer → OnPlayerPaired
    → Consume(reserved→0, ticket缓存删除) → playersMu append → UpdatePlayers(PlayerCount→1, PlayerIps+=[ip1], Broadcast)
```

---

## 八、IP 展示与断开（需求 1）

### 8.1 新 REST 接口

```
POST /api/instance/kickPlayerByIp    body: {"sid": "...", "ip": "10.1.2.3"}
POST /api/instance/kickAllPlayers    body: {"sid": "...", "deny": true}   // deny 默认 true=被踢 IP 进拒绝名单
响应（复用 OkWithDetailed）：{"code":200,"data":{"kicked":2},"msg":"已断开2个连接"}
```

### 8.2 SdpService 新方法

[server/core/service/sdp_service.go](../server/core/service/sdp_service.go)：

```go
func (m *sdpService) KickPlayerByIp(sid, ip string) (int, error) {
    streamer, err := provider.SdpConnProvider.GetStreamer(sid)
    if err != nil { return 0, errors.New("实例未连接Streamer") }
    kicked := 0
    for _, p := range streamer.Players() {          // playersMu 快照
        if p.IP == ip { m.kickOne(streamer, p); kicked++ }
    }
    if kicked == 0 { return 0, errors.New("该IP无已连接玩家") }
    DenyService.Add(ip)                              // 问题 1 规避（第九章）
    return kicked, nil
}

func (m *sdpService) KickAllPlayers(sid string, deny bool) (int, error) {
    streamer, err := provider.SdpConnProvider.GetStreamer(sid)
    if err != nil { return 0, errors.New("实例未连接Streamer") }
    players := streamer.Players()
    for _, p := range players {
        m.kickOne(streamer, p)
        if deny { DenyService.Add(p.IP) }
    }
    return len(players), nil
}

func (m *sdpService) kickOne(streamer *provider.StreamerConnector, p *provider.PlayerConnector) {
    p.SendCloseMsg(4000, "kicked")            // 沿用现有 Kick 关闭码（thinguelib 按 4000 识别被踢）
    streamer.RemovePlayer(p)                  // 立即移出切片 + 通知 streamer playerDisconnected
    streamer.SendPlayersCount()
    InstanceService.UpdatePlayers(streamer)   // PlayerCount/PlayerIps 立即更新 + BroadcastUpdate
    p.Close()
}
```

要点：

- **即时归零**：`kickOne` 主动 `RemovePlayer + UpdatePlayers`，满足"断开后连接数=0、实例变未分配"的即时性（不等读循环退出）。`OnPlayerDisConnect` 随后仍会触发一次 → `RemovePlayer` 查找式删除、找不到即返回 false，**幂等安全**。
- **"未分配"联动**：`UpdatePlayers` 后独占实例 `PlayerCount==0` 且无有效预留 → 分配算法自然重新可分配（与 5.1 联动），管理端经 BroadcastUpdate 自动刷新。
- `KickPlayerUser`（按 UserData 踢人的既有接口，[instance_handler.go:183-196](../server/web/handler/rest/instance_handler.go#L183-L196)）保持不变。

### 8.3 IP 列数据源：镜像字段 `PlayerIps`

`UpdatePlayers`（[instance_service.go:29-45](../server/core/service/instance_service.go#L29-L45)）改造为同一快照生成 `PlayerIds` 与 `PlayerIps`：

```go
func (m *instanceService) UpdatePlayers(streamer *provider.StreamerConnector) *model.ServerInstance {
    m.updateLock.Lock(); defer m.updateLock.Unlock()
    players := streamer.Players()                    // 加锁快照
    playerIds := make(UintSlice, 0, len(players))
    playerIps := make(StringSlice, 0, len(players))
    for _, p := range players {
        playerIds = append(playerIds, p.PlayerId)
        playerIps = append(playerIps, p.IP)
    }
    instance := m.GetInstanceBySid(streamer.SID)
    instance.PlayerIds = playerIds
    instance.PlayerIps = playerIps                   // 新增：与 PlayerIds 同源同快照，原子一致
    instance.PlayerCount = uint(len(players))
    global.SERVER_DB.Save(instance)
    provider.AdminConnProvider.BroadcastUpdate()
    return instance
}
```

选镜像字段而非"前端单独查询接口"的理由：随 `clientList` 一次下发、零额外请求、无额外失败分支；权威数据仍是 `StreamerConnector.PlayerConnectors`，镜像仅展示用；代价只是内存 SQLite 中多一列（可忽略）。

---

## 九、问题 1：断开后客户端自动重连——结论与规避

### 9.1 结论：**会发生**，且有三条独立的重连路径

1. **thinguelib（上游 @thingue/lib-pixelstreamingfrontend，基于 PixelStreaming-Frontend 封装）**：对 dist/thingue.min.js 反编译确认，WebSocket 关闭时存在自动重连逻辑（`shouldReconnect && code != 1001 && MaxReconnectAttempts(默认 3) > 0 → restartStreamAutomatically()`）；**4000 ≠ 1001，被踢后必然重连，且每次重连都重新调用 `playerUrlBuilder` → 重新 ticketSelect → 可能拿到任意实例的新 ticket**（不粘性）。
2. **服务端自带旧播放页**（`server/frontend/public/player_old/scripts/app_v4.js` / `app_v5.js`）：`ws.onclose → is_reconnection=true → setTimeout(start, 4000)` **无限重连**，不区分关闭码。
3. **启动器客户端（/ws/client）**：断开后 2 秒定时重连（[conn_manager.go:67-73](../client/service/server/conn_manager.go#L67-L73)）——但这是"启动器↔server"的**管理连接**，与需求 1 的"断开实例的拉流客户端"无关，**本需求不改它**，仅在此澄清避免误伤。

### 9.2 规避：三层防护

**第一层（权威）——服务端"被踢 IP 拒绝名单"**，新文件 `server/core/service/kick_deny_service.go`：

```go
type denyService struct {
    mu     sync.Mutex
    denied map[string]time.Time   // ip → expireAt
}
var DenyService = &denyService{denied: make(map[string]time.Time)}

func (d *denyService) Add(ip string)          // 写入 now+KickDenySeconds
func (d *denyService) IsDenied(ip string) bool // 惰性清理过期条目后判断
```

- **TTL 配置化**：[config_provider.go](../common/provider/config_provider.go) `LocalServer` 新增 `KickDenySeconds int yaml:"kickDenySeconds"`（**默认 60**，`<=0` 视为不拒绝）。
- **三个检查点**：① `TicketSelect` handler（出票前，重连请求直接 403，最早拦截）→ ② `PlayerWebSocketHandler` upgrade 前（覆盖"先拿票后被踢"窗口）→ ③ `OnPlayerPaired` 复检（兜底）。
- **响应编码**：`common/response` 增加 `DENIED=403` 与 `FailWithCode(code, msg, c)`；ticketSelect 拒绝时返回 `{"code":403,"msg":"已被管理员断开连接，请稍后重试"}`。
- **粒度说明**：拒绝名单按出口 IP 粒度，NAT/公司出口下同 IP 的其他用户共享拒绝窗口（默认 60 秒，可配）；内网直连部署无此问题。被踢者换 IP 重连无法在 IP 粒度阻止（需账号体系，超出本需求）。

**第二层（体验层）——thinguelib 停止自动重连**（见 11.3）：收到 close 4000 或 ticketSelect 403 → 停止重连并派发 `kicked` 事件。上游可能先触发一次重连尝试，该次 ticketSelect 被第一层 403 拦截后终止——服务端名单是权威，SDK 是体验层，双层配合闭环。

**第三层（兜底）**——配对二次校验（7.2），即使拒绝名单被绕过（极小窗口），配对时也拒绝。

### 9.3 命名汇总

| 层面 | 命名 | 说明 |
|---|---|---|
| thinguelib 事件 | `EventType.KICKED = 'kicked'` | 参数 `{code, reason}`；`Core.on('kicked', cb)` 订阅 |
| 服务端 HTTP 码 | `code=403`（`DENIED`） | ticketSelect 拒绝名单拦截 |
| WS 关闭码 | `4000 "kicked"`（现状沿用）/ `4001` / `4002` | 4000=被踢；4001=ticket 无效或过期；4002=实例不可用（未启动/启动超时/已被独占占用）。常量定义见 `server/core/service/sdp_service.go` 的 `ClosePlayer*`；4000~4099 一律由 thinguelib 视为"服务端明确拒绝"并停止自动重连 |
| 拒绝名单配置 | `localServer.kickDenySeconds` | config.yaml，默认 60 |

---

## 十、管理端前端改动（server/frontend）

### 10.1 InstanceList.vue

- `subColumns` 增加 IP 列：`{name:'ips', label:'IP', field: row => row.playerIps || []}`（服务端新字段直接下发）。
- IP 列渲染：每个 IP 一个 `q-chip`，chip 尾部 `q-icon name="close"`（X 键），点击弹确认框（QDialog："确定断开 10.1.2.3 与实例《xxx》的连接？"）→ 调 `kickPlayerByIp({sid, ip})`。
- 操作列追加"全部断开"按钮（`row.playerIps?.length > 0` 才可用）→ 确认框 → `kickAllPlayers({sid})`。
- 连接数列合并显示分配状态：独占实例 `playerCount===0 ? '未分配' : '已占用'`；共享实例显示 `已连接 N`（N=0 时显示 0）。IP 列与状态列随 `emitter('update') → list()` 自动刷新（BroadcastUpdate 链路现状复用）；调用失败用 `Notify.create({type:'negative'})` 展示 `r.msg`。
- `api.js` 新增 `kickPlayerByIp` / `kickAllPlayers` / `updateInstanceSettings` 三个封装。

### 10.2 InstanceInfo.vue（只读 → 可编辑）

在现有只读区（名称/路径/启动参数/标签）下方新增"分配设置"表单区（进入抽屉时从 `props.row` 初始化）：

| 控件 | 规则 |
|---|---|
| 实例类型 `q-option-group` | 共享实例(0) / 独占实例(1)；默认 0 |
| 白名单 `q-input` + chips | 回车/逗号分隔生成 `q-chip`（可删）；前端正则预校验 + 后端 `net.ParseIP` 强校验；空列表提示"不配置=所有 IP 可分配" |
| 连接数上限 `q-input type=number` | 仅共享类型可用；合法值 `-1`（不限）或 `>=1`，`0` 与其它负数非法；独占类型置灰显示"固定 1" |
| 保存按钮 | → `updateInstanceSettings(form)`；成功 Notify + 等 update 广播刷新；失败展示 `msg` |

### 10.3 新 REST：updateInstanceSettings

```
POST /api/instance/updateInstanceSettings
request:  {"sid":"<uuid>","instanceType":0|1,"whitelist":["10.1.2.3"],"maxPlayerCount":-1}
response: 200 data=ServerInstance | 500 msg=校验失败原因
```

服务端 `InstanceService.UpdateInstanceSettings(req)`（[instance_service.go](../server/core/service/instance_service.go) 新增，`updateLock` 内）：

1. 校验：SID 存在；白名单逐个 `net.ParseIP`；上限 `-1 或 >=1`；**共享→独占切换需当前连接数<=1**（否则拒绝："当前连接数超过1，请先断开后再切换为独占"）；
2. 写 SERVER_DB（三字段 `Updates`）；
3. upsert STORAGE_DB `InstanceSettings`（`Clauses(clause.OnConflict)`，last-write-wins）；
4. `BroadcastUpdate()`。

---

## 十一、thinguelib 改动（需求 4、问题 1）

独立仓库 `D:\unreal\projects\uino\thinguelib`。`shared` 参数链路已存在，改动收敛为语义明确化 + 被踢处理：

| 文件 | 改动 |
|---|---|
| `src/ThingUE.js` | ① L115 布尔化：`this.shared = params.shared === undefined ? true : !!params.shared;`（默认 true 不变，存量调用方零感知）；② `EventType` 增 `KICKED: 'kicked'`；③ `urlBuilder`（L514-539）增 403 分支：`resJson.code === 403` → `this.Core.dispatchEvent('kicked', {code:403, reason:resJson.msg}); return '';`（body 中 `shared` 已传，无需改）；④ `initStream` 创建 stream 后注册关闭监听：`e.detail.code === 4000` → `shouldReconnect = false` + 派发 `kicked`（**具体上游 API 路径如 `webRtcController.webSocketController.onClose` 以实际 @thingue/lib-pixelstreamingfrontend 版本为准，实现时验证**） |
| `src/modules/core.js` | L51 同步布尔化；`init` JSDoc（L20-39）中 `shared` 语义改为"true=共享实例（默认）/ false=独占实例" |
| `README.md` / `thingue.api.md` | 参数表补 `shared` 示例（`new ThingUE({url, shared:false})` 独占拉流）、`kicked` 事件说明与 403/4000 行为 |
| `dist/thingue.min.js` | `npm run build` 重建并发布新版本 |

### 11.1 服务端自带播放页与旧页

- `server/frontend/src/player.js`：ticketSelect body（L144-153）补 `shared: urlParams.get("shared") !== "false"`（默认 true），支持 `player.html?sid=xx&shared=false` 测试独占请求。
- `public/player_old/scripts/app_v4.js` / `app_v5.js`：① ticketSelect body 补 `shared: true`；② `ws.onclose` 增 `if (event.code === 4000) { 提示"已被管理员断开"; return; }` 终止无限重连。

---

## 十二、边界与漏洞清单

| # | 场景 | 处理策略 |
|---|---|---|
| 1 | 白名单 IP 格式非法 | 前端正则预校验 + 后端 `net.ParseIP` 强校验，拒绝保存并指明非法项 |
| 2 | IPv6 | `net.ParseIP` 全兼容；比较用规范化字符串（`ParseIP(ip).String()`） |
| 3 | X-Forwarded-For 伪造 | `SetTrustedProxies(nil)`（直连默认）+ `localServer.trustedProxies` 显式配置反代网段；body 自报 IP 一律 `json:"-"` |
| 4 | NAT/公司出口共享 IP | 白名单与拒绝名单以出口 IP 为粒度，文档明示（拒绝窗口默认 60s）；内网直连无此问题 |
| 5 | 运行中修改上限导致当前连接数超限 | 允许保存不追溯踢人；响应附 `currentPlayerCount`，前端提示"当前连接数 N 已超新上限 M，新连接将被拒绝"；分配侧按新上限执行 |
| 6 | 被踢玩家正在重连窗口 | 三层拦截：ticketSelect 403 + upgrade 拒绝 + 配对复检；thinguelib 停自动重连 |
| 7 | ticket 过期未配对 | 预留与 gcache 同 10s 过期（sweep 同步 Remove）；配对 `Consume` 失败 → SendCloseMsg(4001) |
| 8 | 独占玩家断线后立即重连抢到别的实例 | 预期行为（需求未要求粘性）；如需"回原实例优先"可扩展预留表记录 playerId→最近 sid（本期不做） |
| 9 | server 重启 | SERVER_DB 清空、占用归零、连接全断；客户端 2s 重连重注册 → 设置合并恢复；重启窗口内 ticketSelect 可能失败，由上游重连/用户重试 |
| 10 | clientRegister 时 SID 冲突（克隆 config.db） | unique 索引 + register 冲突检测，冲突方换新 UUID（默认设置） |
| 11 | 多管理员并发改配置 | STORAGE_DB 单行 upsert last-write-wins；SERVER_DB 经 updateLock 串行；BroadcastUpdate 最终一致 |
| 12 | 修改白名单对已连接玩家 | 不追溯（新连接生效），文档明示 |
| 13 | kickAll 与并发新连 | kick 用快照 + 名单覆盖窗口期，新 ticket 请求在 TTL 内被 403 |
| 14 | 共享→独占切换时连接数>1 | 后端校验拒绝，提示先断开至 ≤1 |
| 15 | 客户端 body 自报 IP | 忽略（`json:"-"`），永远服务端采集 |
| 16 | 拒绝名单内存增长 | TTL + 惰性清理，无泄漏 |
| 17 | 预留泄漏（拿票不配对） | 10s 过期 + sweep 兜底，同步清 ticket 缓存 |
| 18 | LoadBundle 失败 | 与现状一致（ticket 已发，UE 侧失败不影响信令） |
| 19 | Streamer 断连清理 | `OnStreamerDisconnect` 关全部玩家 → `OnPlayerDisConnect` → `UpdatePlayers` 清零 PlayerIps |
| 20 | PlayerIds/PlayerIps 不同步 | 同一快照同一函数生成；PlayerIps 仅镜像，权威在 PlayerConnectors |
| 21 | deny 名单内玩家已重连成功（极小窗口） | 配对复检兜底，拒绝并关连接 |
| 22 | 被踢者换 IP 重连 | IP 粒度无法阻止（预期内）；需更强控制则引入账号体系（超出本需求） |
| 23 | 独占实例未启动且未开 AutoControl | ready 过滤排除，返回现状文案"实例未启动且未开启自动启停" |
| 24 | 上限脏数据（0 或负数） | 保存接口拒绝（仅 -1 或 >=1）；存量脏数据按不限处理（防御） |
| 25 | 白名单 nil vs 空数组 | `len==0` 统一视为不过滤 |
| 26 | ClientIP 为空（异常） | 不命中任何白名单、不进拒绝名单；日志 warn |
| 27 | 同一 ticket 复用（既有漏洞） | `Consume` 后 `cache.Remove(ticket)`；sweep 同步清理 |
| 28 | 管理端接口无鉴权 | 与现状一致（内网部署假设），kick/updateSettings 与现有管理接口面相同，不新增缺口 |

---

## 十三、改动文件清单

### 13.1 ulauncher 仓库

**common/**
| 文件 | 改/新 | 内容 |
|---|---|---|
| [common/model/instance_server.go](../common/model/instance_server.go) | 改 | +InstanceType/Whitelist/MaxPlayerCount/PlayerIps |
| [common/model/instance_client.go](../common/model/instance_client.go) | 改 | SID 去 `gorm:"-"`（持久化） |
| common/model/instance_settings.go | 新 | STORAGE_DB 设置表 |
| [common/request/request.go](../common/request/request.go) | 改 | SelectorCond：Shared `bool`→`*bool`、+ClientIP(`json:"-"`)；+UpdateInstanceSettingsReq、KickByIpReq |
| common/response/common.go | 改 | +DENIED=403、FailWithCode |
| [common/provider/config_provider.go](../common/provider/config_provider.go) | 改 | LocalServer +KickDenySeconds、+TrustedProxies |

**server/**
| 文件 | 改/新 | 内容 |
|---|---|---|
| server/core/service/ticket_service.go | 改 | 预留结构/resMu/Reserve/Consume/sweep；TicketSelect2 重构（第六/七章）；Order("c_id") |
| server/core/service/kick_deny_service.go | 新 | 被踢 IP 拒绝名单 |
| server/core/service/sdp_service.go | 改 | ConnectStreamer/OnPlayerPaired 校验与 Consume；KickPlayerByIp/KickAllPlayers/kickOne；OnPlayerPaired 幂等 |
| server/core/service/instance_service.go | 改 | UpdatePlayers 同步 PlayerIps（Players() 快照）；+UpdateInstanceSettings |
| server/core/service/client_service.go | 改 | register SID 冲突检测 + 设置合并 + LastSeenAt |
| server/core/provider/player_connector.go | 改 | +IP、+Ticket 字段 |
| server/core/provider/streamer_connector.go | 改 | +playersMu/AddPlayer/RemovePlayer/Players()；裸访问点迁移 |
| server/core/provider/sdp_conn_provider.go | 改 | +mapLock；+GetPlayersByIp |
| server/web/handler/rest/instance_handler.go | 改 | TicketSelect 填 ClientIP + 403；+KickPlayerByIp/KickAllPlayers/UpdateInstanceSettings handler |
| server/web/handler/ws/player_ws_handler.go | 改 | upgrade 前拒绝名单检查；player.IP 填充；4001 关闭路径 |
| server/web/router/instance_router.go | 改 | 注册 3 个新路由 |
| server/initialize/gorm.go | 改 | STORAGE_DB AutoMigrate +InstanceSettings；孤儿行清理 |
| server/initialize/server.go | 改 | SetTrustedProxies（按配置） |

**server/frontend/**
| 文件 | 改/新 | 内容 |
|---|---|---|
| src/api.js | 改 | +kickPlayerByIp/kickAllPlayers/updateInstanceSettings |
| src/components/InstanceList.vue | 改 | IP 列（chips+X）、全部断开、分配状态列 |
| src/components/InstanceInfo.vue | 改 | 类型/白名单/上限表单 + 保存 + 校验 |
| src/player.js | 改 | +shared 参数 |
| public/player_old/scripts/app_v4.js、app_v5.js | 改 | +shared:true；onclose 4000 分支 |

### 13.2 thinguelib 仓库（D:\unreal\projects\uino\thinguelib）

| 文件 | 改/新 | 内容 |
|---|---|---|
| src/ThingUE.js | 改 | shared 布尔化、KICKED 事件、urlBuilder 403 分支、onClose 4000 停重连 |
| src/modules/core.js | 改 | shared 布尔化、JSDoc |
| README.md、thingue.api.md | 改 | 参数/事件文档 |
| dist/thingue.min.js | 重建 | npm run build 发布 |

---

## 十四、实施顺序与验证方案

### 14.1 阶段划分（每阶段独立可验证、不互相阻塞）

**P0 数据模型与持久化**（model / client_service / gorm.go / SID 持久化）
- 验证：启动服务 → 客户端重连 → `curl http://127.0.0.1:8877/api/instance/instanceList` 看新字段默认值（`instanceType:0, maxPlayerCount:-1, whitelist:null, playerIps:null`）；重启 server 与启动器各一次，前后 instanceList 的 sid 相同（SID 已持久化）。

**P1 分配算法与预留**（ticket_service / request / handler TicketSelect）
- curl 用例（License 有效前提下）：
  ```bash
  # 独占请求（无独占实例 → 报错）
  curl -s -X POST http://127.0.0.1:8877/api/instance/ticketSelect \
    -H "Content-Type: application/json" -d '{"shared": false}'
  # → {"code":500,"msg":"没有可用的独占实例",...}
  # 共享请求（默认第一个共享实例，ticket 正常）
  curl -s -X POST http://127.0.0.1:8877/api/instance/ticketSelect \
    -H "Content-Type: application/json" -d '{"shared": true}'
  # 旧请求（不传 shared，应走共享池+旧排序，不回归）
  curl -s -X POST http://127.0.0.1:8877/api/instance/ticketSelect \
    -H "Content-Type: application/json" -d '{"name":"xxx"}'
  ```
- 并发用例：两个终端同时循环 `ticketSelect {"shared":false}` 对同一独占实例 → 恰好一个成功、一个报"独占实例已全部被占用"；ticket 超过 10 秒后再连 ws 应被 4001 拒绝；同一 ticket 二次使用被拒绝（既有漏洞已堵）。
- 白名单用例：设置实例 A 白名单 `[10.1.2.3]` 后，从其它 IP 请求应落到实例 B（第二段）。

**P2 IP 采集与按 IP 断开**（player_connector / streamer_connector / sdp_service / instance_service / handler / router / 前端列表）
- 验证：播放页连接后 `instanceList` 中 `playerIps` 出现该 IP；`curl -X POST .../kickPlayerByIp -d '{"sid":"...","ip":"10.1.2.3"}'` → 播放页断开、`playerIps` 立即清空、连接数 0；管理端"全部断开"按钮路径同上；`OnPlayerDisConnect` 重复触发不报错（幂等）。

**P3 拒绝名单（问题 1）**（kick_deny_service / TicketSelect 403 / upgrade / 配对复检 / config）
- 验证：踢掉播放端后立即重复 `ticketSelect` → `{"code":403,"msg":"已被管理员断开连接，请稍后重试"}`；直接用旧 ticket 连 ws 被拒；60s 后恢复；`kickDenySeconds: 0` 时立即恢复。

**P4 管理端设置面板**（InstanceInfo / InstanceList / api.js / updateInstanceSettings）
- 操作路径：实例列表 → 展开 → 设置按钮 → 抽屉内：切类型、加白名单 chip、输上限 → 保存 → 提示成功 → 列表刷新显示；非法输入（IP 写错、上限 0、共享→独占且连接数>1）被前后端拦下；重启 server 后设置仍存在（STORAGE_DB 持久化 + 重注册合并）。

**P5 thinguelib 发版**（ThingUE.js / core.js / README / build / push）
- 验证：`new ThingUE({url, shared:false})` 走独占请求；被管理端踢掉 → 触发 `kicked` 事件、不再自动重连；`shared` 不传 → 默认共享。

**P6 player.js 补参 + player_old 4000 分支 + 全量回归**
- 回归点清单：
  1. License 校验不受影响；
  2. SceneId 四级优先级（带场景的旧流程）仍可用且叠加类型/白名单/容量过滤；
  3. labelSelector 路径；
  4. SID 直选（player.html?sid=）；
  5. Streamer 断连/重启时玩家清理与 PlayerIps 归零；
  6. 自动启停（AutoControl+StopDelay）在全部断开后仍正常触发；
  7. `kickPlayerUser`（按 UserData）旧接口不受影响；
  8. 启动器客户端重连重注册后配置合并；
  9. 共享上限满时新连接被拒、已连接不受影响。

**推荐顺序**：P0 → P1 → P2 → P3 → P4 → P5 → P6（P5/P6 可与 P4 并行）。

---

## 十五、需求覆盖对照与解读说明

### 15.1 需求逐条覆盖

| 需求 | 方案章节 |
|---|---|
| 1. IP 展示与剔除（X 断开/全断开/归零变未分配） | 五（IP 采集）、八（接口与踢人）、10.1（前端） |
| 2. 实例白名单（设置面板，可空） | 四（字段与持久化）、10.2（面板）、10.3（保存接口）、六（分配） |
| 3. 实例类型与连接数（独占固定1 / 共享默认-1可设上限） | 四（字段）、10.2（面板）、六（容量判据） |
| 4. 客户端请求参数（独占/共享） | 6.2（三态）、十一（thinguelib） |
| 5.1 独占分配策略 | 6.3 + 6.5 + 6.6 |
| 5.2 共享分配策略 | 6.3 + 6.5 + 6.6 |
| 问题 1（断开后重连） | 九（结论+三层规避） |

### 15.2 需求解读说明（请评审确认）

1. **"按顺序"** = 实例配置顺序（`c_id` 升序；跨客户端按客户端列表顺序）。理由：CID 是启动器客户端本地自增主键，即实例在配置中的顺序，管理端列表同序展示。
2. **白名单命中但已满/已占用时回落**：采用宽松语义（跳过、可回落到无白名单实例，全部无可分配才报错）。理由：与 5.1 独占的逐实例条件表述一致，避免白名单用户被整体拒绝。若需严格语义（命中白名单即不回落），可后续在设置接口增加开关。
3. **携带 sceneId 时保留场景四级优先级**（级内 c_id 序）：兼容现有 thinguelib 场景加载行为（优先复用已加载场景实例）；不携带 sceneId 的请求严格按"按顺序"执行。如需携带 sceneId 也完全按 c_id 序，改动点集中一处（6.5）。
4. **旧播放页（未传 shared）按共享请求 + 旧排序处理**，但仍受类型池（仅共享型）、白名单、容量约束——保证不回归，同时无法借旧页面绕过白名单与独占限制。
