package provider

import (
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	"os"
	"path"
	"thingue-launcher/common/constants"
	"thingue-launcher/common/logger"
)

var AppConfig = new(Config)

var configFile = path.Join(constants.SAVE_DIR, constants.CONFIG_NAME)

type Config struct {
	ServerURL             string         `json:"serverURL" yaml:"serverURL"`
	LocalServer           LocalServer    `json:"localServer" yaml:"localServer"`
	SystemSettings        SystemSettings `json:"systemSettings" yaml:"systemSettings"`
	PeerConnectionOptions string         `json:"peerConnectionOptions" yaml:"peerConnectionOptions"`
	ModelLibrary          ModelLibrary   `json:"modelLibrary" yaml:"modelLibrary"` // 模型库配置
}

type LocalServer struct {
	ContentPath       string `json:"contentPath" yaml:"contentPath"`
	BindAddr          string `json:"bindAddr" yaml:"bindAddr"`
	AutoStart         bool   `json:"autoStart" yaml:"autoStart"`
	UseExternalStatic bool   `json:"useExternalStatic" yaml:"useExternalStatic"`
	StaticDir         string `json:"staticDir" yaml:"staticDir"`
}

type SystemSettings struct {
	EnableRestartTask  bool   `json:"enableRestartTask" yaml:"enableRestartTask"`
	RestartTaskCron    string `json:"restartTaskCron" yaml:"restartTaskCron"`
	ExternalEditorPath string `json:"externalEditorPath" yaml:"externalEditorPath"`
	LogLevel           string `json:"logLevel" yaml:"logLevel"`
}

type ModelLibrary struct {
	Enabled   bool   `json:"enabled" yaml:"enabled"`         // 是否启用模型库功能
	ModelsDir string `json:"modelsDir" yaml:"modelsDir"`     // 模型存储目录
	MaxUploadSize int64 `json:"maxUploadSize" yaml:"maxUploadSize"` // 最大上传文件大小(MB)
}

func InitConfigFromFile() {
	viper.SetDefault("serverURL", "http://localhost:8877/")
	viper.SetDefault("localServer.bindAddr", "0.0.0.0:8877")
	viper.SetDefault("localServer.contentPath", "/")
	viper.SetDefault("localServer.useExternalStatic", false)
	viper.SetDefault("localServer.staticDir", constants.SAVE_DIR+"static")
	viper.SetDefault("localServer.autoStart", true)
	viper.SetDefault("systemSettings.logLevel", "info")
	// 模型库默认配置
	viper.SetDefault("modelLibrary.enabled", true) // 默认开启
	viper.SetDefault("modelLibrary.modelsDir", constants.SAVE_DIR+"static/models")
	viper.SetDefault("modelLibrary.maxUploadSize", 100) // 默认100MB
	// 加载配置文件
	viper.SetConfigFile(configFile)
	_ = viper.ReadInConfig()
	viper.Unmarshal(&AppConfig)
}

func WriteConfigToFile() {
	_, err := os.Stat(configFile)
	if os.IsNotExist(err) {
		err := os.MkdirAll(path.Dir(configFile), 0755)
		file, err := os.Create(configFile)
		defer file.Close()
		if err != nil {
			panic(err)
		}
	}
	data, err := yaml.Marshal(&AppConfig)
	err = os.WriteFile(configFile, data, 0755)
	if err != nil {
		logger.Zap.Error(err)
	}
}
