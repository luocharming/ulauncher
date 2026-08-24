package initialize

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"thingue-launcher/common/logger"
	"time"
	"thingue-launcher/common/model"
	"thingue-launcher/common/provider"
	coreprovider "thingue-launcher/server/core/provider"
	coreservice "thingue-launcher/server/core/service"
	"thingue-launcher/server/global"
	"thingue-launcher/server/web/router"
)

type server struct {
	listen            *http.Server
	IsRunning         bool
	CloseReturnChanel chan string
	router            *gin.Engine
	isInitialized     bool
}

var Server = new(server)

func (s *server) Serve() {
	var err error
	//ORM+MQTT
	if !s.isInitialized { //如果是第一次没有初始化
		//s.router = router.BuildRouter(s.StaticFiles) //构建路由
		initServerDB() // 初始化gorm
		initStorageDB()
		initMqttServer()

		// 自动导入现有模型到数据库
		if provider.AppConfig.ModelLibrary.Enabled {
			logger.Zap.Info("开始扫描并导入现有模型...")
			count, err := coreservice.ModelServiceInstance.ImportExistingModels()
			if err != nil {
				logger.Zap.Error("导入模型失败", err)
			} else {
				logger.Zap.Info("模型导入完成，共导入 ", count, " 个模型")
			}
		}

		s.isInitialized = true
		appCode := coreservice.LicenseService.GenerateApplicationCode()
		logger.Zap.Infof("License申请码: %s", appCode)
		if err := coreservice.LicenseService.EnsureAuthorized(); err != nil {
			logger.Zap.Warn("授权状态: ", err.Error())
		} else {
			logger.Zap.Info("授权状态：有效")
		}
	}
	global.SERVER_DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Client{})
	global.SERVER_DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.ServerInstance{})
	// 清理孤儿 InstanceSettings：客户端删除实例/重装后超过 30 天未再 register 的设置行
	global.STORAGE_DB.Where("last_seen_at < ?", time.Now().AddDate(0, 0, -30)).Delete(&model.InstanceSettings{})
	// 构建gin路由
	s.router = router.BuildRouter()
	// Listen
	s.listen = &http.Server{
		Addr:    provider.AppConfig.LocalServer.BindAddr,
		Handler: s.router,
	}
	s.IsRunning = true
	logger.Zap.Info("thingue server listening at: ", s.listen.Addr)
	err = s.listen.ListenAndServe() //运行中阻塞
	s.IsRunning = false
	if s.CloseReturnChanel != nil {
		s.CloseReturnChanel <- err.Error()
	}
	logger.Zap.Info("server closed", err)
}

func (s *server) Start() {
	if s.IsRunning {
		return
	}
	go func() {
		s.Serve()
	}()
}

func (s *server) Stop() {
	err := s.listen.Close()
	coreprovider.ClientConnProvider.CloseAllConnection()
	coreprovider.AdminConnProvider.CloseAllConnection()
	if err != nil {
		logger.Zap.Error("server shutdown failed", err)
	} else {
		logger.Zap.Info("server gracefully stopped")
	}
}
