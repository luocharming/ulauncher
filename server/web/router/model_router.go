package router

import (
	"github.com/gin-gonic/gin"
	"thingue-launcher/server/web/handler"
)

type modelRouter struct{}

var ModelRouter = new(modelRouter)

func (m *modelRouter) BuildRouter(Router *gin.RouterGroup) gin.IRoutes {
	modelRouter := Router.Group("model")
	{
		modelRouter.GET("list", handler.ModelGroup.GetModels)                      // 获取模型列表
		modelRouter.GET("detail/:uuid", handler.ModelGroup.GetModelDetail)         // 获取模型详情
		modelRouter.GET("thumbnail/:uuid/:size", handler.ModelGroup.GetThumbnail)  // 获取缩略图
		modelRouter.GET("download/:uuid", handler.ModelGroup.DownloadModel)        // 下载模型（ZIP打包）
		modelRouter.GET("versions/:uuid", handler.ModelGroup.GetVersions)          // 获取版本列表
		modelRouter.POST("upload", handler.ModelGroup.UploadModel)                 // 上传模型
		modelRouter.POST("import", handler.ModelGroup.ImportModels)                // 导入现有模型
		modelRouter.DELETE("delete/:uuid", handler.ModelGroup.DeleteModel)         // 删除模型

		// UE运行时加载接口
		modelRouter.GET("resource/:uuid", handler.ModelGroup.GetModelResource)                    // 获取模型资源信息（含URL）
		modelRouter.GET("file/:uuid/:version/:filename", handler.ModelGroup.GetModelFile)         // 下载单个资源文件
		modelRouter.POST("resources/batch", handler.ModelGroup.GetModelResourcesBatch)            // 批量获取模型资源
	}
	return modelRouter
}
