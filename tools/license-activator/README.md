# License Activator (GUI)

独立的授权激活工具（Go + Fyne）。

功能：
- 展示本机申请码（可复制）。
- 粘贴激活码（JSON），保存到指定路径（默认 `./license/x.lic`）。
- 本地预检激活文件有效性（验签与过期判断），离线完成。

环境变量：
- `THINGUE_LICENSE_PUBKEY` 或 `X_LICENSE_PUBKEY`：Ed25519 公钥（Base64）。

说明：
- 工具完全独立，不依赖服务端代码，不耦合现有模块。
- 签发侧请使用与服务端一致的消息拼装方式：`fingerprint|license_type|issued_at|expires_at|features(csv)` 进行 Ed25519 签名。

## 编译构建

- 前置要求：
  - 已安装 `Go 1.21+`，启用 `CGO`（Windows 下建议安装 `TDM-GCC` 或 `MSYS2` 以满足 C 编译环境）。
  - 拉取依赖时网络可访问 `proxy.golang.org` 或已配置 `GOPROXY`。

### 使用 make 一键构建（推荐）

- 进入目录：`cd tools\license-activator`
- 首次使用请先整理依赖：`make deps`
- 构建 Windows 版本（输出到 `dist/license-activator-windows-amd64.exe`）：
  - `make build-win`
- 构建 Linux 版本（输出到 `dist/license-activator-linux-amd64`）：
  - `make build-linux`
- 同时构建两者：
  - `make`
- 清理：
  - `make clean`

注意：
- Windows 需安装 GNU Make（可通过 MSYS2/Git Bash/Chocolatey/winget 安装），并在支持 CGO 的终端中运行。
- 交叉构建（例如在 Windows 构建 Linux 可执行文件）需要对应平台的 C 工具链；建议在目标平台或 WSL 中执行。

### 不使用 make（纯 go 指令）

- Windows：
  - `cd tools\license-activator && go build -o license-activator.exe`
- Linux/macOS：
  - `cd tools/license-activator && go build -o license-activator`

## 运行与使用

1. 设置公钥环境变量（必需）：
   - Windows（PowerShell）：
     - `Set-Item -Path Env:THINGUE_LICENSE_PUBKEY -Value "<Ed25519公钥Base64>"`
   - Linux/macOS：
     - `export THINGUE_LICENSE_PUBKEY="<Ed25519公钥Base64>"`
   - 也可使用 `X_LICENSE_PUBKEY` 变量。

2. 启动激活工具：
   - Windows：双击 `license-activator.exe` 或在目录中执行 `./license-activator.exe`
   - Linux/macOS：`./license-activator`

3. 复制申请码：
   - 打开应用后，点击“复制申请码”，将申请码发送给发行方进行离线签发。

4. 粘贴激活文件 JSON：
   - 将发行方返回的激活文件 JSON 复制到“粘贴激活文件 JSON 内容”区域。

5. 保存激活文件：
   - 默认保存到 `./license/x.lic`（相对于当前工作目录）。
   - 如需自定义保存位置，可在路径框中修改，或在服务端通过 `THINGUE_LICENSE_PATH` 指定读取路径。

6. 预检有效性（可选）：
   - 点击“预检激活有效性”，工具会使用当前环境的公钥进行验签、校验指纹与过期时间，显示验证结果。

## 激活文件示例（JSON）

```json
{
  "fingerprint": "HOSTNAME|windows|amd64|AA-BB-CC-DD-EE-FF",
  "license_type": "standard",
  "issued_at": 1730000000,
  "expires_at": 1760000000,
  "features": ["webrtc", "max_instances:10"],
  "signature": "<Base64-Ed25519-Signature>"
}
```

## 签名原文说明（签发侧）

- 按以下格式拼装待签名字符串（请确保字段与顺序严格一致）：
  - `fingerprint|license_type|issued_at|expires_at|features(csv)`
- 其中 `features(csv)` 为 `features` 数组使用英文逗号拼接的字符串，示例：`webrtc,max_instances:10`。
- 使用 Ed25519 私钥对该字符串进行签名，并将签名结果以 Base64 编码后填入 `signature` 字段。

## 环境变量设置示例

- Windows（PowerShell）：
  - 设置公钥：`$env:THINGUE_LICENSE_PUBKEY = "<Base64公钥>"`
  - 指定读取路径：`$env:THINGUE_LICENSE_PATH = "C:\\path\\to\\x.lic"`
  - 清除变量：`Remove-Item Env:THINGUE_LICENSE_PUBKEY`

- Linux/macOS：
  - 设置公钥：`export THINGUE_LICENSE_PUBKEY="<Base64公钥>"`
  - 指定读取路径：`export THINGUE_LICENSE_PATH="/path/to/x.lic"`
  - 清除变量：`unset THINGUE_LICENSE_PUBKEY`

## 常见问题

- “公钥未配置或格式错误”：未设置或设置了非 Base64 的 Ed25519 公钥。
- “签名缺失或格式错误”：`signature` 字段为空或不是合法 Base64。
- “激活码无效（验签失败）”：签发端与校验端的签名原文格式不一致，或公钥不匹配。
- “激活码不匹配当前设备”：`fingerprint` 与本机计算的指纹不同，需在目标机器上签发或重新生成申请码。
- “授权已过期”：`expires_at` 小于当前时间，需重新签发新激活码。
