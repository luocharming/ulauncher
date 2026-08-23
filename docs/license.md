技术方案.md

背景与现状

- 当前服务通过 TicketSelect / TicketSelect2 从数据库筛选可用的 ServerInstance ，生成一次性的 ticket ，并用 gcache 以 10 秒 TTL 临时映射 ticket -> SID 。
- 选择逻辑支持 SID 、 Name 、 PlayerCount 、 StreamerConnected 、 LabelSelector 及携带 SceneId/Shared 场景的优先策略；可触发 PakControl 加载资源包。
- 需求：在发放 ticket 前增加授权校验。未授权或授权过期时拒绝发放 ticket 并返回明确错误。
目标与原则

- 简单可靠：以“当前机器硬件指纹”生成申请码，离线签发激活码，指定目录放置激活文件即生效。
- 低侵入：仅在 ticketService.TicketSelect() 和 ticketService.TicketSelect2() 前置一次授权校验，其余业务不受影响。
- 安全可控：签名验真与到期校验，避免伪造与篡改。
- 可运维：提供 GUI 激活工具展示申请码、写入激活文件；支持默认目录与环境变量覆盖。
设计概览

- 新增授权模块 LicenseManager （服务端）：
  - 采集硬件指纹，生成申请码（展示/导出）。
  - 解析指定目录的激活文件，校验证书签名与有效期。
  - 提供 EnsureAuthorized() 在发放 ticket 前拦截。
- 激活文件：
  - JSON（或轻量文本）格式，包含目标机器指纹、过期时间、许可类型、签名。
  - 由发行方使用私钥对许可主体签名；服务端使用内置公钥验签。
- 激活工具（Go GUI）：
  - 展示本机申请码，支持复制/导出。
  - 粘贴激活码并保存至指定目录，提供本地有效性预检。
核心实现

- 硬件指纹与申请码
  
  - 指纹采集：
    - 收集首个非回环、非虚拟网卡的 MAC 地址（ net.Interfaces() ），同时拼接 Hostname 、 OS/Arch ；若需更强绑定可再采集磁盘/CPU 信息，但建议保持“简单且跨平台”。
    - 指纹规范化：去空格、统一大小写、排序后拼接。
  - 指纹摘要：
    - fingerprint = SHA256(joined_hardware_info) ；申请码编码使用 Base32 或 Base64 （便于复制）。
  - 展示/导出：
    - 在服务启动日志中打印申请码（仅信息日志）。
    - 提供 GUI 工具显示同一申请码；必要时提供 CLI 输出。
- 激活码与许可证文件
  
  - 激活文件格式（建议 JSON）：
    - 关键字段： fingerprint （目标机器）、 license_type 、 issued_at 、 expires_at （UTC）、 features （可选）、 signature （对前述字段的签名）。
  - 签名算法：
    - 推荐 Ed25519 （小型、易用）或 RSA-2048 ；服务端内置公钥，脱离网络即可校验。
  - 签发流程（离线）：
    - 运营方工具读取用户提交的申请码，生成包含到期时间等的许可主体，使用私钥签名，返回激活文件。
  - 放置目录与识别：
    - 默认路径： ./license/x.lic （相对服务工作目录）。
- 服务器端授权校验
  
  - LicenseManager 结构：
    - GenerateApplicationCode() string ：返回当前机器申请码。
    - LoadAndVerify(path string) (LicenseStatus, error) ：读取许可文件、验签、校验到期。
    - EnsureAuthorized() error ：对当前进程授权状态进行判断，返回详细错误：
      - 未找到许可文件 -> “未授权”
      - 验签失败 -> “激活码无效”
      - 已过期 -> “授权已过期”
  - 状态缓存与刷新（简化方案）：
    - 启动时加载一次许可；每次 ticketSelect 调用前再快速校验缓存状态与到期时间。
    - 简化起见可不做文件变更监听；变更后重启服务或提供手动“Reload License”接口（可选）。
- TicketSelect 拦截点
  
  - 在 TicketSelect / TicketSelect2 开始处调用 LicenseManager.EnsureAuthorized() ：
    - 返回错误时直接中断，按现有模式返回 error （消息为中文，如“未授权”或“授权已过期”）。
    - 成功时继续执行原有筛选与发放逻辑。
  - 其他接口（如 GetSidByTicket ）不必拦截，维持最小改动。
- 日志与错误返回
  
  - 日志：在授权失败时记录原因与申请码摘要（不泄露完整私密信息）。
  - 错误消息：
    - 未授权： errors.New("未授权：未检测到激活文件")
    - 激活码无效： errors.New("未授权：激活码无效")
    - 授权过期： errors.New("授权已过期")
激活工具（Go GUI）

- 功能与流程
  - 展示本机申请码（文本框支持复制）。
  - 粘贴激活码（或选择激活文件），指定保存目录（默认 ./license/x.lic ）。
  - 保存后尝试本地校验：读取刚写入的文件并用同一公钥验签、校验到期（离线）。
  - 显示结果提示：成功/失败原因。
- 技术选型
  - 优先 Wails （项目已使用，易于集成和构建 Windows GUI）；若需更轻量可选 Fyne 。
- 交互要点
  - 只提供申请码展示与文件写入，不负责签发；由运营方工具生成激活码。
  - 可选：一键打开激活目录；勾选“覆盖已有文件”。
运维与部署

- 默认目录与环境变量
  - 默认激活路径： ./license/x.lic 。
- 发行与更新流程
  - 管理员运行 GUI 工具获取申请码，提交给运营方。
  - 运营方离线签发，返回激活文件。
  - 管理员将文件放至指定目录，重启服务或触发“Reload License”（如实现）。
- 安全注意事项
  - 服务端仅内置公钥；私钥严格保密在签发端。
  - 激活文件包含到期时间并签名；任何修改将导致验签失败。
  - 日志避免输出完整指纹与文件内容；仅输出短摘要。
最小改动清单（代码改动点）

- 新增： server/core/service/license_service.go
  - LicenseManager （生成申请码、加载和验证激活文件、对外 EnsureAuthorized() ）。
  - 读取环境变量 x_LICENSE_PATH ；缺省到 ./license/x.lic 。
  - 内置公钥常量（或通过配置文件注入）。
- 修改： server/core/service/ticket_service.go
  - TicketSelect() / TicketSelect2() 开头调用 LicenseManager.EnsureAuthorized() ，失败直接返回错误。
- 修改 server/initialize/server.go
  - 启动时日志打印申请码（帮助运维获取申请码）。
  - 启动时尝试加载激活文件并打印授权状态。
测试方案

- 单元测试
  - 指纹生成稳定性测试（不同网卡/顺序，确保一致）。
  - 验签测试（有效签名、篡改字段、到期时间判定）。
- 集成测试
  - 无激活文件： TicketSelect 返回“未授权”。
  - 无效激活文件：返回“激活码无效”。
  - 过期激活文件：返回“授权已过期”。
  - 有效激活文件：正常返回 ticket 。
- GUI 测试
  - 申请码展示一致性与复制功能。
  - 激活文件写入路径和覆盖行为。
  - 本地预检结果与服务端行为一致。
后续扩展

- 许可维度扩展：并发实例数限制、功能模块开关（在 features 中定义）。
- 文件监听与热加载：在不重启的情况下更新授权状态。
- 授权审计：记录授权状态变化与近期校验结果。
- 双授权源：文件授权 + 云端授权（后续可选）。