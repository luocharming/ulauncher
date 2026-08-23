# UE运行时模型加载方案设计

## 方案概述

为UE C++运行时提供HTTP接口，支持动态下载、挂载PAK文件并实例化资产。

## API设计

### 1. 获取模型资源URL（新增）

**接口**: `GET /api/model/resource/:uuid`

**功能**: 返回模型的所有资源文件URL和元数据，供UE C++加载

**请求参数**:
- `uuid` (路径参数): 模型UUID
- `version` (查询参数，可选): 版本号，不指定则返回最新版本

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
      "pak": "http://server:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/asset.pak",
      "utoc": "http://server:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/asset.utoc",
      "ucas": "http://server:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/asset.ucas",
      "metadata": "http://server:8877/api/model/file/6d12d3a7-4d31-e136-0987-5bbac665a1b5/v1.0.0/metadata.json"
    },
    "size_bytes": 26108,
    "description": ""
  }
}
```

### 2. 下载单个资源文件（新增）

**接口**: `GET /api/model/file/:uuid/:version/:filename`

**功能**: 下载指定模型的单个资源文件（pak/utoc/ucas/metadata.json）

**请求参数**:
- `uuid` (路径参数): 模型UUID
- `version` (路径参数): 版本号（格式：v1.0.0）
- `filename` (路径参数): 文件名（asset.pak/asset.utoc/asset.ucas/metadata.json）

**响应**:
- Content-Type: application/octet-stream
- 文件二进制数据

**支持HTTP Range请求**（用于断点续传和分块下载）

### 3. 批量获取模型资源（新增）

**接口**: `POST /api/model/resources/batch`

**功能**: 批量获取多个模型的资源URL

**请求体**:
```json
{
  "models": [
    {"uuid": "uuid1", "version": "1.0.0"},
    {"uuid": "uuid2"}  // 不指定version则使用最新版本
  ]
}
```

**响应**: 返回模型资源URL数组

## UE C++加载流程

### 步骤1: 获取模型资源信息

```cpp
// 1. 发送HTTP请求获取模型资源信息
FString URL = FString::Printf(TEXT("http://server:8877/api/model/resource/%s"), *ModelUUID);
TSharedRef<IHttpRequest> Request = FHttpModule::Get().CreateRequest();
Request->SetURL(URL);
Request->SetVerb(TEXT("GET"));
Request->OnProcessRequestComplete().BindLambda([](FHttpRequestPtr Request, FHttpResponsePtr Response, bool bSuccess) {
    if (bSuccess && Response.IsValid()) {
        // 解析JSON响应
        TSharedPtr<FJsonObject> JsonObject;
        TSharedRef<TJsonReader<>> Reader = TJsonReaderFactory<>::Create(Response->GetContentAsString());
        if (FJsonSerializer::Deserialize(Reader, JsonObject)) {
            TSharedPtr<FJsonObject> Data = JsonObject->GetObjectField(TEXT("data"));
            FString PakURL = Data->GetObjectField(TEXT("resources"))->GetStringField(TEXT("pak"));
            FString UtocURL = Data->GetObjectField(TEXT("resources"))->GetStringField(TEXT("utoc"));
            FString UcasURL = Data->GetObjectField(TEXT("resources"))->GetStringField(TEXT("ucas"));
            FString AssetPath = Data->GetStringField(TEXT("asset_path"));
            FString MountPoint = Data->GetStringField(TEXT("pak_mount_point"));

            // 继续下载PAK文件
            DownloadAndMountPak(PakURL, UtocURL, UcasURL, MountPoint, AssetPath);
        }
    }
});
Request->ProcessRequest();
```

### 步骤2: 下载PAK文件

```cpp
void DownloadAndMountPak(const FString& PakURL, const FString& UtocURL, const FString& UcasURL,
                         const FString& MountPoint, const FString& AssetPath) {
    // 下载到临时目录
    FString TempDir = FPaths::ProjectSavedDir() / TEXT("DownloadedPaks");
    FString PakPath = TempDir / FPaths::GetCleanFilename(PakURL);

    // 使用HTTP下载PAK文件
    TSharedRef<IHttpRequest> PakRequest = FHttpModule::Get().CreateRequest();
    PakRequest->SetURL(PakURL);
    PakRequest->SetVerb(TEXT("GET"));
    PakRequest->OnProcessRequestComplete().BindLambda([PakPath, MountPoint, AssetPath]
        (FHttpRequestPtr Request, FHttpResponsePtr Response, bool bSuccess) {
        if (bSuccess && Response.IsValid()) {
            // 保存PAK文件
            FFileHelper::SaveArrayToFile(Response->GetContent(), *PakPath);

            // 挂载PAK
            MountPakFile(PakPath, MountPoint, AssetPath);
        }
    });
    PakRequest->ProcessRequest();

    // 同样下载utoc和ucas文件（IoStore格式需要）
    // ... 类似的下载逻辑
}
```

### 步骤3: 挂载PAK文件

```cpp
void MountPakFile(const FString& PakPath, const FString& MountPoint, const FString& AssetPath) {
    // 获取PAK平台文件
    FPakPlatformFile* PakPlatform = (FPakPlatformFile*)(FPlatformFileManager::Get()
        .FindPlatformFile(FPakPlatformFile::GetTypeName()));

    if (PakPlatform) {
        // 挂载PAK文件
        FPakFile PakFile(PakPlatform->GetLowerLevel(), *PakPath, false);
        if (PakFile.IsValid()) {
            PakPlatform->Mount(*PakPath, 0, *MountPoint);
            UE_LOG(LogTemp, Log, TEXT("PAK mounted successfully: %s"), *PakPath);

            // 加载资产
            LoadAssetFromPak(AssetPath);
        }
    }
}
```

### 步骤4: 加载并实例化资产

```cpp
void LoadAssetFromPak(const FString& AssetPath) {
    // 异步加载资产
    FStreamableManager& Streamable = UAssetManager::GetStreamableManager();
    FSoftObjectPath SoftPath(AssetPath);

    Streamable.RequestAsyncLoad(SoftPath, [SoftPath]() {
        UObject* LoadedAsset = SoftPath.ResolveObject();
        if (LoadedAsset) {
            // 如果是蓝图类
            if (UClass* BlueprintClass = Cast<UClass>(LoadedAsset)) {
                // 实例化蓝图
                AActor* SpawnedActor = GetWorld()->SpawnActor<AActor>(BlueprintClass);
                UE_LOG(LogTemp, Log, TEXT("Blueprint spawned: %s"), *SpawnedActor->GetName());
            }
            // 如果是静态网格体
            else if (UStaticMesh* StaticMesh = Cast<UStaticMesh>(LoadedAsset)) {
                // 创建静态网格体组件
                UStaticMeshComponent* MeshComp = NewObject<UStaticMeshComponent>(this);
                MeshComp->SetStaticMesh(StaticMesh);
                MeshComp->RegisterComponent();
            }
        }
    });
}
```

## 完整的UE C++加载器类

```cpp
// ModelLoader.h
#pragma once

#include "CoreMinimal.h"
#include "Http.h"
#include "JsonObjectConverter.h"

USTRUCT()
struct FModelResourceInfo {
    GENERATED_BODY()

    UPROPERTY()
    FString UUID;

    UPROPERTY()
    FString Name;

    UPROPERTY()
    FString Version;

    UPROPERTY()
    FString AssetPath;

    UPROPERTY()
    FString PakMountPoint;

    UPROPERTY()
    FString PakURL;

    UPROPERTY()
    FString UtocURL;

    UPROPERTY()
    FString UcasURL;
};

class FModelLoader {
public:
    // 从服务器加载模型
    void LoadModelFromServer(const FString& ServerURL, const FString& ModelUUID,
                            TFunction<void(AActor*)> OnComplete);

private:
    void FetchModelInfo(const FString& URL, TFunction<void(FModelResourceInfo)> OnComplete);
    void DownloadPakFiles(const FModelResourceInfo& Info, TFunction<void(FString)> OnComplete);
    void MountAndLoadAsset(const FString& PakPath, const FModelResourceInfo& Info,
                          TFunction<void(AActor*)> OnComplete);
};

// ModelLoader.cpp
void FModelLoader::LoadModelFromServer(const FString& ServerURL, const FString& ModelUUID,
                                       TFunction<void(AActor*)> OnComplete) {
    FString URL = FString::Printf(TEXT("%s/api/model/resource/%s"), *ServerURL, *ModelUUID);

    FetchModelInfo(URL, [this, OnComplete](FModelResourceInfo Info) {
        DownloadPakFiles(Info, [this, Info, OnComplete](FString PakPath) {
            MountAndLoadAsset(PakPath, Info, OnComplete);
        });
    });
}
```

## 优化建议

### 1. 缓存机制
- 在UE端缓存已下载的PAK文件
- 使用版本号判断是否需要重新下载
- 实现LRU缓存策略，自动清理旧版本

### 2. 断点续传
- 后端支持HTTP Range请求
- UE端实现分块下载和断点续传
- 大文件下载更可靠

### 3. 预加载
- 提前下载常用模型
- 后台异步下载，不阻塞游戏运行

### 4. 安全性
- 添加Token认证
- 验证文件完整性（MD5/SHA256）
- HTTPS加密传输

## 实现优先级

1. **高优先级**:
   - 实现 `/api/model/resource/:uuid` 接口
   - 实现 `/api/model/file/:uuid/:version/:filename` 接口
   - 提供基础的UE C++加载示例

2. **中优先级**:
   - 支持HTTP Range请求
   - 添加文件完整性校验
   - 实现批量获取接口

3. **低优先级**:
   - 缓存策略
   - 预加载机制
   - Token认证
