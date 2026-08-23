package rest

import (
	"fmt"
	"net/http"
	"thingue-launcher/common/response"
	"thingue-launcher/server/core/service"

	"github.com/gin-gonic/gin"
)

type ModelGroup struct{}

// GetModels 获取模型列表
func (g *ModelGroup) GetModels(c *gin.Context) {
	// 解析分页参数
	page, pageSize := service.ParsePageParams(c.Query("page"), c.Query("pageSize"))

	// 解析筛选条件
	filters := make(map[string]interface{})
	if modelType := c.Query("type"); modelType != "" {
		filters["type"] = modelType
	}
	if category := c.Query("category"); category != "" {
		filters["category"] = category
	}
	if tags := c.Query("tags"); tags != "" {
		filters["tags"] = tags
	}
	if name := c.Query("name"); name != "" {
		filters["name"] = name
	}

	// 调用服务层
	result, err := service.ModelServiceInstance.ListModels(page, pageSize, filters)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	response.OkWithData(result, c)
}

// GetModelDetail 获取模型详情
func (g *ModelGroup) GetModelDetail(c *gin.Context) {
	uuid := c.Param("uuid")
	version := c.Query("version")

	modelAsset, err := service.ModelServiceInstance.GetModelDetail(uuid, version)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	response.OkWithData(modelAsset, c)
}

// GetThumbnail 获取缩略图
func (g *ModelGroup) GetThumbnail(c *gin.Context) {
	uuid := c.Param("uuid")
	size := c.Param("size")
	version := c.Query("version")

	data, err := service.ModelServiceInstance.GetThumbnail(uuid, version, size)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	c.Data(http.StatusOK, "image/png", data)
}

// DownloadModel 下载模型
func (g *ModelGroup) DownloadModel(c *gin.Context) {
	uuid := c.Param("uuid")
	version := c.Query("version")

	data, filename, err := service.ModelServiceInstance.DownloadModel(uuid, version)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "application/zip", data)
}

// UploadModel 上传模型
func (g *ModelGroup) UploadModel(c *gin.Context) {
	// 解析multipart form
	form, err := c.MultipartForm()
	if err != nil {
		response.FailWithMessage("解析表单失败: "+err.Error(), c)
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		response.FailWithMessage("未上传任何文件", c)
		return
	}

	// 调用服务层
	modelAsset, err := service.ModelServiceInstance.UploadModel(files)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	response.OkWithDetailed(modelAsset, "上传成功", c)
}

// DeleteModel 删除模型
func (g *ModelGroup) DeleteModel(c *gin.Context) {
	uuid := c.Param("uuid")
	version := c.Query("version")

	err := service.ModelServiceInstance.DeleteModel(uuid, version)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	if version != "" {
		response.OkWithMessage("版本 "+version+" 删除成功", c)
	} else {
		response.OkWithMessage("模型删除成功", c)
	}
}

// GetVersions 获取模型的所有版本
func (g *ModelGroup) GetVersions(c *gin.Context) {
	uuid := c.Param("uuid")

	versions, err := service.ModelServiceInstance.GetVersions(uuid)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	response.OkWithData(versions, c)
}

// ImportModels 导入现有的模型文件到数据库
func (g *ModelGroup) ImportModels(c *gin.Context) {
	count, err := service.ModelServiceInstance.ImportExistingModels()
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	response.OkWithDetailed(map[string]interface{}{
		"count": count,
	}, fmt.Sprintf("成功导入 %d 个模型", count), c)
}

// ===== UE运行时加载接口 =====

// GetModelResource 获取模型资源信息（含URL）- 供UE C++运行时使用
func (g *ModelGroup) GetModelResource(c *gin.Context) {
	uuid := c.Param("uuid")
	version := c.Query("version")

	resource, err := service.ModelServiceInstance.GetModelResource(uuid, version, c.Request.Host)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	response.OkWithData(resource, c)
}

// GetModelFile 下载单个资源文件 - 供UE C++运行时使用
func (g *ModelGroup) GetModelFile(c *gin.Context) {
	uuid := c.Param("uuid")
	version := c.Param("version")
	filename := c.Param("filename")

	data, contentType, err := service.ModelServiceInstance.GetModelFile(uuid, version, filename)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	// 支持HTTP Range请求（断点续传）
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%s", filename))
	c.Data(http.StatusOK, contentType, data)
}

// GetModelResourcesBatch 批量获取模型资源信息 - 供UE C++运行时使用
func (g *ModelGroup) GetModelResourcesBatch(c *gin.Context) {
	var req struct {
		Models []struct {
			UUID    string `json:"uuid"`
			Version string `json:"version"`
		} `json:"models" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage("请求参数错误: "+err.Error(), c)
		return
	}

	resources, err := service.ModelServiceInstance.GetModelResourcesBatch(req.Models, c.Request.Host)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}

	response.OkWithData(resources, c)
}
