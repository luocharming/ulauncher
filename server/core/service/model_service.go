package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/model"
	"thingue-launcher/common/provider"
	"thingue-launcher/common/response"
	"thingue-launcher/server/global"

	"github.com/Masterminds/semver/v3"
)

type ModelService struct{}

var ModelServiceInstance = new(ModelService)

// ListModels 获取模型列表（支持分页和筛选）
func (s *ModelService) ListModels(page, pageSize int, filters map[string]interface{}) (*response.PageResult[*model.ModelAsset], error) {
	var models []*model.ModelAsset
	var total int64

	// 构建查询
	query := global.STORAGE_DB.Model(&model.ModelAsset{})

	// 应用筛选条件
	if modelType, ok := filters["type"].(string); ok && modelType != "" {
		query = query.Where("type = ?", modelType)
	}
	if category, ok := filters["category"].(string); ok && category != "" {
		query = query.Where("category = ?", category)
	}
	if tags, ok := filters["tags"].(string); ok && tags != "" {
		// 标签筛选（JSON数组字符串模糊匹配）
		query = query.Where("tags LIKE ?", "%"+tags+"%")
	}
	if name, ok := filters["name"].(string); ok && name != "" {
		// 名称模糊搜索
		query = query.Where("name LIKE ?", "%"+name+"%")
	}

	// 统计总数
	query.Count(&total)

	// 分页查询
	offset := (page - 1) * pageSize
	err := query.Order("modified_time DESC").Offset(offset).Limit(pageSize).Find(&models).Error
	if err != nil {
		logger.Zap.Error("查询模型列表失败", err)
		return nil, err
	}

	return &response.PageResult[*model.ModelAsset]{
		List:     models,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// GetModelDetail 获取模型详情
func (s *ModelService) GetModelDetail(uuid string, version string) (*model.ModelAsset, error) {
	var modelAsset model.ModelAsset

	query := global.STORAGE_DB.Where("uuid = ?", uuid)
	if version != "" {
		query = query.Where("version = ?", version)
	} else {
		// 如果未指定版本，获取最新版本
		query = query.Order("version DESC")
	}

	err := query.First(&modelAsset).Error
	if err != nil {
		logger.Zap.Error("查询模型详情失败", err)
		return nil, err
	}

	return &modelAsset, nil
}

// UploadModel 上传模型
func (s *ModelService) UploadModel(files []*multipart.FileHeader) (*model.ModelAsset, error) {
	// 1. 查找并解析metadata.json
	var metadataFile *multipart.FileHeader
	for _, file := range files {
		if file.Filename == "metadata.json" {
			metadataFile = file
			break
		}
	}

	if metadataFile == nil {
		return nil, errors.New("缺少metadata.json文件")
	}

	// 2. 解析元数据
	metadata, err := s.parseMetadata(metadataFile)
	if err != nil {
		return nil, fmt.Errorf("解析元数据失败: %v", err)
	}

	// 3. 验证必需文件
	requiredFiles := []string{"asset.pak", "asset.utoc", "asset.ucas", "thumbnail_64x64.png", "thumbnail_256x256.png"}
	fileMap := make(map[string]*multipart.FileHeader)
	for _, file := range files {
		fileMap[file.Filename] = file
	}

	for _, required := range requiredFiles {
		if _, exists := fileMap[required]; !exists {
			return nil, fmt.Errorf("缺少必需文件: %s", required)
		}
	}

	// 4. 检查版本是否已存在
	var existingModel model.ModelAsset
	err = global.STORAGE_DB.Where("uuid = ? AND version = ?", metadata.UUID, metadata.Version).First(&existingModel).Error
	if err == nil {
		return nil, fmt.Errorf("版本 %s 已存在", metadata.Version)
	}

	// 5. 保存文件到文件系统
	versionDir := s.getVersionPath(metadata.UUID, metadata.Version)
	err = os.MkdirAll(versionDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("创建版本目录失败: %v", err)
	}

	for _, file := range files {
		destPath := filepath.Join(versionDir, file.Filename)
		err = s.saveUploadedFile(file, destPath)
		if err != nil {
			// 清理已创建的文件
			os.RemoveAll(versionDir)
			return nil, fmt.Errorf("保存文件失败: %v", err)
		}
	}

	// 6. 保存到数据库
	err = global.STORAGE_DB.Create(metadata).Error
	if err != nil {
		// 清理文件
		os.RemoveAll(versionDir)
		return nil, fmt.Errorf("保存到数据库失败: %v", err)
	}

	logger.Zap.Info(fmt.Sprintf("模型上传成功: %s v%s", metadata.Name, metadata.Version))
	return metadata, nil
}

// DownloadModel 下载模型（ZIP压缩）
func (s *ModelService) DownloadModel(uuid string, version string) ([]byte, string, error) {
	logger.Zap.Info("开始下载模型", "uuid", uuid, "version", version)

	// 1. 查询模型信息
	modelAsset, err := s.GetModelDetail(uuid, version)
	if err != nil {
		logger.Zap.Error("查询模型详情失败", err, "uuid", uuid, "version", version)
		return nil, "", err
	}

	// 2. 获取版本目录
	versionDir := s.getVersionPath(uuid, modelAsset.Version)
	logger.Zap.Info("模型版本目录", "versionDir", versionDir)

	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		logger.Zap.Error("模型文件不存在", err, "versionDir", versionDir)
		return nil, "", errors.New("模型文件不存在")
	}

	// 3. 创建ZIP压缩包
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// 遍历目录中的所有文件
	fileCount := 0
	err = filepath.Walk(versionDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Zap.Error("遍历文件失败", err, "path", path)
			return err
		}
		if info.IsDir() {
			return nil
		}

		// 读取文件
		fileData, err := os.ReadFile(path)
		if err != nil {
			logger.Zap.Error("读取文件失败", err, "path", path)
			return fmt.Errorf("读取文件失败 %s: %v", path, err)
		}

		// 获取相对路径
		relPath, err := filepath.Rel(versionDir, path)
		if err != nil {
			logger.Zap.Error("获取相对路径失败", err, "path", path, "versionDir", versionDir)
			return err
		}

		// 转换为ZIP兼容路径（将Windows反斜杠转换为正斜杠）
		// ZIP文件格式标准要求使用正斜杠作为路径分隔符
		zipPath := filepath.ToSlash(relPath)

		// 添加到ZIP
		writer, err := zipWriter.Create(zipPath)
		if err != nil {
			logger.Zap.Error("创建ZIP条目失败", err, "zipPath", zipPath)
			return err
		}

		_, err = writer.Write(fileData)
		if err != nil {
			logger.Zap.Error("写入ZIP内容失败", err, "zipPath", zipPath)
			return err
		}

		fileCount++
		logger.Zap.Debug("文件已添加到ZIP", "zipPath", zipPath, "size", len(fileData))
		return nil
	})

	if err != nil {
		logger.Zap.Error("创建ZIP失败", err, "fileCount", fileCount)
		return nil, "", fmt.Errorf("创建ZIP失败: %v", err)
	}

	logger.Zap.Info("所有文件已添加到ZIP", "fileCount", fileCount)

	err = zipWriter.Close()
	if err != nil {
		logger.Zap.Error("关闭ZIP失败", err)
		return nil, "", fmt.Errorf("关闭ZIP失败: %v", err)
	}

	filename := fmt.Sprintf("%s_v%s.zip", uuid, modelAsset.Version)
	zipSize := buf.Len()
	logger.Zap.Info("ZIP创建成功", "filename", filename, "size", zipSize, "sizeFormatted", FormatFileSize(int64(zipSize)))

	return buf.Bytes(), filename, nil
}

// DeleteModel 删除模型或指定版本
func (s *ModelService) DeleteModel(uuid string, version string) error {
	if version != "" {
		// 删除指定版本
		var modelAsset model.ModelAsset
		err := global.STORAGE_DB.Where("uuid = ? AND version = ?", uuid, version).First(&modelAsset).Error
		if err != nil {
			return errors.New("版本不存在")
		}

		// 删除文件
		versionDir := s.getVersionPath(uuid, version)
		err = os.RemoveAll(versionDir)
		if err != nil {
			logger.Zap.Error("删除版本目录失败", err)
		}

		// 删除数据库记录
		err = global.STORAGE_DB.Delete(&modelAsset).Error
		if err != nil {
			return fmt.Errorf("删除数据库记录失败: %v", err)
		}

		logger.Zap.Info(fmt.Sprintf("版本 %s 删除成功", version))
	} else {
		// 删除整个模型（所有版本）
		var models []model.ModelAsset
		err := global.STORAGE_DB.Where("uuid = ?", uuid).Find(&models).Error
		if err != nil {
			return errors.New("模型不存在")
		}

		if len(models) == 0 {
			return errors.New("模型不存在")
		}

		// 删除文件
		modelDir := s.getModelPath(uuid)
		err = os.RemoveAll(modelDir)
		if err != nil {
			logger.Zap.Error("删除模型目录失败", err)
		}

		// 删除数据库记录
		err = global.STORAGE_DB.Where("uuid = ?", uuid).Delete(&model.ModelAsset{}).Error
		if err != nil {
			return fmt.Errorf("删除数据库记录失败: %v", err)
		}

		logger.Zap.Info(fmt.Sprintf("模型 %s 删除成功", uuid))
	}

	return nil
}

// GetThumbnail 获取缩略图
func (s *ModelService) GetThumbnail(uuid string, version string, size string) ([]byte, error) {
	// 1. 查询模型信息
	modelAsset, err := s.GetModelDetail(uuid, version)
	if err != nil {
		return nil, err
	}

	// 2. 确定缩略图文件名
	var thumbnailFile string
	if size == "64x64" {
		thumbnailFile = "thumbnail_64x64.png"
	} else if size == "256x256" {
		thumbnailFile = "thumbnail_256x256.png"
	} else {
		return nil, errors.New("无效的缩略图尺寸")
	}

	// 3. 读取缩略图文件
	thumbnailPath := filepath.Join(s.getVersionPath(uuid, modelAsset.Version), thumbnailFile)
	data, err := os.ReadFile(thumbnailPath)
	if err != nil {
		return nil, fmt.Errorf("读取缩略图失败: %v", err)
	}

	return data, nil
}

// GetVersions 获取模型的所有版本（排序）
func (s *ModelService) GetVersions(uuid string) ([]string, error) {
	var models []model.ModelAsset
	err := global.STORAGE_DB.Where("uuid = ?", uuid).Find(&models).Error
	if err != nil {
		return nil, err
	}

	versions := make([]string, len(models))
	for i, m := range models {
		versions[i] = m.Version
	}

	// 使用semver排序
	sort.Slice(versions, func(i, j int) bool {
		v1, err1 := semver.NewVersion(versions[i])
		v2, err2 := semver.NewVersion(versions[j])
		if err1 != nil || err2 != nil {
			return versions[i] < versions[j]
		}
		return v1.LessThan(v2)
	})

	return versions, nil
}

// ===== 辅助方法 =====

// getModelPath 获取模型目录路径
func (s *ModelService) getModelPath(uuid string) string {
	return filepath.Join(provider.AppConfig.ModelLibrary.ModelsDir, uuid)
}

// getVersionPath 获取版本目录路径
func (s *ModelService) getVersionPath(uuid string, version string) string {
	return filepath.Join(s.getModelPath(uuid), "v"+version)
}

// parseMetadata 解析metadata.json文件
func (s *ModelService) parseMetadata(file *multipart.FileHeader) (*model.ModelAsset, error) {
	// 打开文件
	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	// 读取内容
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}

	// 解析JSON
	var metadata model.ModelAssetMetadata
	err = json.Unmarshal(data, &metadata)
	if err != nil {
		return nil, err
	}

	// 转换为ModelAsset
	tagsJSON, _ := json.Marshal(metadata.Metadata.Tags)
	modelAsset := &model.ModelAsset{
		UUID:          metadata.AssetInfo.UUID,
		Name:          metadata.AssetInfo.Name,
		Type:          metadata.AssetInfo.Type,
		Path:          metadata.AssetInfo.Path,
		Version:       metadata.AssetInfo.Version,
		Category:      metadata.Metadata.Category,
		Tags:          string(tagsJSON),
		Description:   metadata.AssetInfo.Description,
		Author:        metadata.Metadata.Author,
		SizeBytes:     metadata.FileInfo.SizeBytes,
		VertexCount:   metadata.Geometry.VertexCount,
		TriangleCount: metadata.Geometry.TriangleCount,
		MaterialCount: metadata.Geometry.MaterialCount,
		PakFile:       metadata.FileInfo.PakFile,
		PakMountPoint: metadata.FileInfo.PakMountPoint,
		CreatedTime:   metadata.AssetInfo.CreatedTime,
		ModifiedTime:  metadata.AssetInfo.ModifiedTime,
		Thumbnail64:   "/static/models/" + metadata.AssetInfo.UUID + "/v" + metadata.AssetInfo.Version + "/thumbnail_64x64.png",
		Thumbnail256:  "/static/models/" + metadata.AssetInfo.UUID + "/v" + metadata.AssetInfo.Version + "/thumbnail_256x256.png",
	}

	return modelAsset, nil
}

// saveUploadedFile 保存上传的文件
func (s *ModelService) saveUploadedFile(file *multipart.FileHeader, destPath string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// compareVersions 比较两个版本号
func (s *ModelService) compareVersions(v1, v2 string) int {
	ver1, err1 := semver.NewVersion(v1)
	ver2, err2 := semver.NewVersion(v2)

	if err1 != nil || err2 != nil {
		// 如果解析失败，使用字符串比较
		return strings.Compare(v1, v2)
	}

	if ver1.LessThan(ver2) {
		return -1
	} else if ver1.Equal(ver2) {
		return 0
	}
	return 1
}

// FormatFileSize 格式化文件大小
func FormatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ParsePageParams 解析分页参数
func ParsePageParams(pageStr, pageSizeStr string) (int, int) {
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	pageSize, err := strconv.Atoi(pageSizeStr)
	if err != nil || pageSize < 1 {
		pageSize = 12
	}
	if pageSize > 100 {
		pageSize = 100
	}

	return page, pageSize
}

// ImportExistingModels 扫描并导入现有的模型文件到数据库
func (s *ModelService) ImportExistingModels() (int, error) {
	modelsDir := provider.AppConfig.ModelLibrary.ModelsDir
	if _, err := os.Stat(modelsDir); os.IsNotExist(err) {
		return 0, fmt.Errorf("模型目录不存在: %s", modelsDir)
	}

	importCount := 0

	// 遍历模型目录
	uuidDirs, err := os.ReadDir(modelsDir)
	if err != nil {
		return 0, fmt.Errorf("读取模型目录失败: %v", err)
	}

	for _, uuidDir := range uuidDirs {
		if !uuidDir.IsDir() {
			continue
		}

		uuid := uuidDir.Name()
		uuidPath := filepath.Join(modelsDir, uuid)

		// 遍历版本目录
		versionDirs, err := os.ReadDir(uuidPath)
		if err != nil {
			logger.Zap.Error(fmt.Sprintf("读取UUID目录失败: %s", uuid), err)
			continue
		}

		for _, versionDir := range versionDirs {
			if !versionDir.IsDir() || !strings.HasPrefix(versionDir.Name(), "v") {
				continue
			}

			version := strings.TrimPrefix(versionDir.Name(), "v")
			versionPath := filepath.Join(uuidPath, versionDir.Name())

			// 检查是否已存在
			var existingModel model.ModelAsset
			err = global.STORAGE_DB.Where("uuid = ? AND version = ?", uuid, version).First(&existingModel).Error
			if err == nil {
				logger.Zap.Info(fmt.Sprintf("模型已存在，跳过: %s v%s", uuid, version))
				continue
			}

			// 读取metadata.json
			metadataPath := filepath.Join(versionPath, "metadata.json")
			metadataData, err := os.ReadFile(metadataPath)
			if err != nil {
				logger.Zap.Error(fmt.Sprintf("读取metadata.json失败: %s", metadataPath), err)
				continue
			}

			// 解析元数据
			var metadata model.ModelAssetMetadata
			err = json.Unmarshal(metadataData, &metadata)
			if err != nil {
				logger.Zap.Error(fmt.Sprintf("解析metadata.json失败: %s", metadataPath), err)
				continue
			}

			// 转换为ModelAsset
			tagsJSON, _ := json.Marshal(metadata.Metadata.Tags)
			modelAsset := &model.ModelAsset{
				UUID:          metadata.AssetInfo.UUID,
				Name:          metadata.AssetInfo.Name,
				Type:          metadata.AssetInfo.Type,
				Path:          metadata.AssetInfo.Path,
				Version:       metadata.AssetInfo.Version,
				Category:      metadata.Metadata.Category,
				Tags:          string(tagsJSON),
				Description:   metadata.AssetInfo.Description,
				Author:        metadata.Metadata.Author,
				SizeBytes:     metadata.FileInfo.SizeBytes,
				VertexCount:   metadata.Geometry.VertexCount,
				TriangleCount: metadata.Geometry.TriangleCount,
				MaterialCount: metadata.Geometry.MaterialCount,
				PakFile:       metadata.FileInfo.PakFile,
				PakMountPoint: metadata.FileInfo.PakMountPoint,
				CreatedTime:   metadata.AssetInfo.CreatedTime,
				ModifiedTime:  metadata.AssetInfo.ModifiedTime,
				Thumbnail64:   "/static/models/" + metadata.AssetInfo.UUID + "/v" + metadata.AssetInfo.Version + "/thumbnail_64x64.png",
				Thumbnail256:  "/static/models/" + metadata.AssetInfo.UUID + "/v" + metadata.AssetInfo.Version + "/thumbnail_256x256.png",
			}

			// 保存到数据库
			err = global.STORAGE_DB.Create(modelAsset).Error
			if err != nil {
				logger.Zap.Error(fmt.Sprintf("保存模型到数据库失败: %s v%s", uuid, version), err)
				continue
			}

			logger.Zap.Info(fmt.Sprintf("导入模型成功: %s v%s", modelAsset.Name, version))
			importCount++
		}
	}

	return importCount, nil
}

// ===== UE运行时加载接口 =====

// ModelResourceInfo UE运行时模型资源信息
type ModelResourceInfo struct {
	UUID           string            `json:"uuid"`
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Type           string            `json:"type"`
	AssetPath      string            `json:"asset_path"`
	PakMountPoint  string            `json:"pak_mount_point"`
	Resources      map[string]string `json:"resources"`
	SizeBytes      int64             `json:"size_bytes"`
	Description    string            `json:"description"`
}

// GetModelResource 获取模型资源信息（含URL）
func (s *ModelService) GetModelResource(uuid string, version string, host string) (*ModelResourceInfo, error) {
	// 1. 查询模型信息
	modelAsset, err := s.GetModelDetail(uuid, version)
	if err != nil {
		return nil, err
	}

	// 2. 构建资源URL
	baseURL := fmt.Sprintf("http://%s/api/model/file/%s/v%s", host, uuid, modelAsset.Version)

	resources := map[string]string{
		"pak":      fmt.Sprintf("%s/asset.pak", baseURL),
		"utoc":     fmt.Sprintf("%s/asset.utoc", baseURL),
		"ucas":     fmt.Sprintf("%s/asset.ucas", baseURL),
		"metadata": fmt.Sprintf("%s/metadata.json", baseURL),
	}

	// 3. 构建响应
	resourceInfo := &ModelResourceInfo{
		UUID:          modelAsset.UUID,
		Name:          modelAsset.Name,
		Version:       modelAsset.Version,
		Type:          modelAsset.Type,
		AssetPath:     modelAsset.Path,
		PakMountPoint: modelAsset.PakMountPoint,
		Resources:     resources,
		SizeBytes:     modelAsset.SizeBytes,
		Description:   modelAsset.Description,
	}

	logger.Zap.Info("获取模型资源信息", "uuid", uuid, "version", modelAsset.Version)
	return resourceInfo, nil
}

// GetModelFile 获取单个模型文件
func (s *ModelService) GetModelFile(uuid string, version string, filename string) ([]byte, string, error) {
	// 1. 验证文件名（安全检查，防止路径遍历攻击）
	allowedFiles := map[string]string{
		"asset.pak":     "application/octet-stream",
		"asset.utoc":    "application/octet-stream",
		"asset.ucas":    "application/octet-stream",
		"metadata.json": "application/json",
	}

	contentType, allowed := allowedFiles[filename]
	if !allowed {
		return nil, "", fmt.Errorf("不允许访问的文件: %s", filename)
	}

	// 2. 去掉版本号前缀 "v"
	versionClean := strings.TrimPrefix(version, "v")

	// 3. 构建文件路径
	filePath := filepath.Join(s.getVersionPath(uuid, versionClean), filename)

	// 4. 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logger.Zap.Error("文件不存在", err, "filePath", filePath)
		return nil, "", fmt.Errorf("文件不存在: %s", filename)
	}

	// 5. 读取文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		logger.Zap.Error("读取文件失败", err, "filePath", filePath)
		return nil, "", fmt.Errorf("读取文件失败: %v", err)
	}

	logger.Zap.Info("获取模型文件", "uuid", uuid, "version", version, "filename", filename, "size", len(data))
	return data, contentType, nil
}

// GetModelResourcesBatch 批量获取模型资源信息
func (s *ModelService) GetModelResourcesBatch(models []struct {
	UUID    string `json:"uuid"`
	Version string `json:"version"`
}, host string) ([]*ModelResourceInfo, error) {
	resources := make([]*ModelResourceInfo, 0, len(models))

	for _, m := range models {
		resource, err := s.GetModelResource(m.UUID, m.Version, host)
		if err != nil {
			logger.Zap.Error("获取模型资源失败", err, "uuid", m.UUID, "version", m.Version)
			// 跳过失败的模型，继续处理其他模型
			continue
		}
		resources = append(resources, resource)
	}

	logger.Zap.Info("批量获取模型资源", "requested", len(models), "success", len(resources))
	return resources, nil
}
