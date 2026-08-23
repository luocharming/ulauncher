# UE运行时模型加载 - 使用指南

## 快速开始

### 1. 后端API接口

已实现以下接口供UE C++运行时使用：

#### 获取模型资源信息
```
GET /api/model/resource/:uuid?version=1.0.0
```

**响应示例**:
```json
{
  "code": 200,
  "data": {
    "uuid": "6d12d3a7-4d31-e136-0987-5bbac665a1b5",
    "name": "BP_Point",
    "version": "1.0.0",
    "type": "Blueprint",
    "asset_path": "/Game/Project/Blueprints/Points/BP_Point.BP_Point",
    "pak_mount_point": "/Game/",
    "resources": {
      "pak": "http://localhost:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/asset.pak",
      "utoc": "http://localhost:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/asset.utoc",
      "ucas": "http://localhost:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/asset.ucas",
      "metadata": "http://localhost:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/metadata.json"
    },
    "size_bytes": 26108,
    "description": ""
  }
}
```

#### 下载单个资源文件
```
GET /api/model/file/:uuid/:version/:filename
```

支持的文件：
- `asset.pak` - PAK文件
- `asset.utoc` - IoStore目录文件
- `asset.ucas` - IoStore容器文件
- `metadata.json` - 元数据文件

**特性**:
- 支持HTTP Range请求（断点续传）
- 自动设置正确的Content-Type
- 路径安全验证，防止目录遍历攻击

#### 批量获取模型资源
```
POST /api/model/resources/batch
Content-Type: application/json

{
  "models": [
    {"uuid": "uuid1", "version": "1.0.0"},
    {"uuid": "uuid2"}
  ]
}
```

### 2. UE C++集成

#### 方式1: 使用提供的加载器类

将 `ModelRuntimeLoader.h` 和 `ModelRuntimeLoader.cpp` 添加到您的UE项目中。

**C++使用示例**:
```cpp
// 创建加载器实例
UModelRuntimeLoader* Loader = NewObject<UModelRuntimeLoader>();

// 设置回调
FScriptDelegate OnComplete;
OnComplete.BindUFunction(this, "OnModelLoaded");

FScriptDelegate OnProgress;
OnProgress.BindUFunction(this, "OnLoadProgress");

FScriptDelegate OnError;
OnError.BindUFunction(this, "OnLoadError");

// 开始加载
Loader->LoadModelFromServer(
    TEXT("http://localhost:8877"),
    TEXT("6d12d3a7-4d31-e136-0987-5bbac665a1b5"),
    OnComplete,
    OnProgress,
    OnError
);
```

**蓝图使用示例**:
```
1. 在蓝图中创建 ModelRuntimeLoader 对象
2. 调用 LoadModelFromServer 节点
3. 绑定 OnComplete、OnProgress、OnError 事件
4. 等待加载完成
```

#### 方式2: 手动实现

参考 `ue_runtime_loading.md` 中的详细步骤：

1. 发送HTTP请求获取模型资源信息
2. 下载PAK、UTOC、UCAS文件到本地
3. 使用 `FPakPlatformFile::Mount()` 挂载PAK
4. 使用 `FStreamableManager` 异步加载资产
5. 实例化蓝图或静态网格体

### 3. 测试步骤

#### 测试后端接口

1. **启动服务器**:
```bash
cd build/bin
./thingue-launcher.exe
```

2. **测试获取模型资源信息**:
```bash
curl http://localhost:8877/api/model/resource/6d12d3a7-4d31-e136-0987-5bbac665a1b5
```

3. **测试下载PAK文件**:
```bash
curl -O http://localhost:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/asset.pak
```

4. **验证文件大小**:
```bash
ls -lh asset.pak
# 应该显示正确的文件大小（如 5.0K）
```

#### 测试UE加载

1. **在UE编辑器中测试**:
   - 创建一个测试Actor
   - 在BeginPlay中调用加载器
   - 运行游戏，观察日志输出

2. **检查日志**:
```
LogTemp: Fetching model info from: http://localhost:8877/api/model/resource/...
LogTemp: Model info fetched: BP_Point v1.0.0
LogTemp: Downloading PAK files to: ...
LogTemp: Downloaded pak file (1/3)
LogTemp: Downloaded utoc file (2/3)
LogTemp: Downloaded ucas file (3/3)
LogTemp: Mounting PAK: ... at /Game/
LogTemp: PAK mounted successfully
LogTemp: Loading asset: /Game/Project/Blueprints/Points/BP_Point.BP_Point
LogTemp: Asset loaded: ...
LogTemp: Blueprint spawned: BP_Point_C_0
LogTemp: Model loading completed successfully
```

3. **验证结果**:
   - 检查场景中是否生成了Actor
   - 验证Actor的类型和属性
   - 测试多次加载（应使用缓存）

### 4. 常见问题

#### Q: 下载的ZIP文件只有1KB且损坏
**A**: 已修复！问题出在前端代码使用了 `response` 而不是 `response.data`。现在已修复为：
```javascript
const url = window.URL.createObjectURL(new Blob([response.data]));
```

#### Q: PAK文件无法挂载
**A**: 检查以下几点：
1. PAK文件是否完整下载（检查文件大小）
2. UTOC和UCAS文件是否都已下载
3. 挂载点路径是否正确（如 `/Game/`）
4. PAK文件是否使用正确的UE版本打包

#### Q: 资产加载失败
**A**:
1. 检查资产路径是否正确（使用完整路径，如 `/Game/Path/To/Asset.Asset`）
2. 确认PAK已成功挂载
3. 检查资产注册表是否已扫描新挂载的路径
4. 查看UE日志中的详细错误信息

#### Q: 如何支持断点续传
**A**: 后端已支持HTTP Range请求。在UE端实现：
```cpp
Request->SetHeader(TEXT("Range"), FString::Printf(TEXT("bytes=%lld-"), ResumePosition));
```

#### Q: 如何实现缓存管理
**A**: 加载器已实现基础缓存：
- 缓存路径：`ProjectSaved/ModelCache/{UUID}/v{Version}/`
- 自动检测已缓存的模型
- 可扩展LRU缓存策略

### 5. 性能优化建议

1. **预加载常用模型**:
```cpp
// 在游戏启动时预加载
TArray<FString> CommonModels = {
    TEXT("uuid1"),
    TEXT("uuid2"),
    TEXT("uuid3")
};

for (const FString& UUID : CommonModels)
{
    Loader->LoadModelFromServer(ServerURL, UUID, ...);
}
```

2. **使用批量接口**:
```cpp
// 一次请求获取多个模型信息
POST /api/model/resources/batch
```

3. **实现资源池**:
```cpp
// 复用已加载的资产，避免重复加载
TMap<FString, UObject*> LoadedAssets;
```

4. **后台下载**:
```cpp
// 在加载屏幕或空闲时下载
AsyncTask(ENamedThreads::AnyBackgroundThreadNormalTask, [this]() {
    DownloadPakFiles(...);
});
```

### 6. 安全性考虑

1. **文件名验证**: 后端已实现白名单验证，只允许访问特定文件
2. **路径遍历防护**: 使用 `filepath.Join()` 安全构建路径
3. **建议添加**:
   - Token认证
   - 文件完整性校验（MD5/SHA256）
   - HTTPS加密传输
   - 访问频率限制

### 7. 下一步开发

- [ ] 实现HTTP Range支持（分块下载）
- [ ] 添加文件完整性校验
- [ ] 实现LRU缓存策略
- [ ] 添加Token认证
- [ ] 支持增量更新
- [ ] 实现资源预加载队列
- [ ] 添加下载速度限制
- [ ] 支持CDN加速

## 相关文档

- [ue_runtime_loading.md](./ue_runtime_loading.md) - 详细的技术设计文档
- [ModelRuntimeLoader.h](./ModelRuntimeLoader.h) - UE加载器头文件
- [ModelRuntimeLoader.cpp](./ModelRuntimeLoader.cpp) - UE加载器实现

## 联系支持

如有问题，请查看日志输出或联系开发团队。
