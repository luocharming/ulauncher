package model

import (
	"thingue-launcher/common/domain"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/labels"
)

type ServerInstance struct {
	CID                  uint                `json:"cid" gorm:"primarykey"`
	ClientID             uint                `json:"ClientID" gorm:"primarykey"`
	SID                  string              `json:"sid" gorm:"unique"`
	SceneId              string              `json:"sceneId"`
	Name                 string              `json:"name"`
	ExecPath             string              `json:"execPath"`
	LaunchArguments      StringSlice         `json:"launchArguments"`
	Metadata             string              `json:"metadata"`
	PaksConfig           string              `json:"paksConfig"`
	FaultRecover         bool                `json:"faultRecover"`
	EnableRelay          bool                `json:"enableRelay"`
	EnableRenderControl  bool                `json:"enableRenderControl"`
	EnableAutoStart	  	 bool                `json:"enableAutoStart"`
	EnableSharedInstance bool                `json:"enableSharedInstance"`
	// ---- 实例分配（需求2/3）：持久化来源为 STORAGE_DB 的 InstanceSettings，register 时合并 ----
	InstanceType   int8        `json:"instanceType" gorm:"default:0"`    // 0=共享(默认) 1=独占
	Whitelist      StringSlice `json:"whitelist"`                        // 白名单 IP；空=不过滤
	MaxPlayerCount int         `json:"maxPlayerCount" gorm:"default:-1"` // 共享连接数上限：-1=不限(默认)；独占忽略(恒按1)
	PlayerIps      StringSlice `json:"playerIps"`                        // 已连接客户端 IP 镜像（权威数据在 PlayerConnectors）
	LastStartAt          time.Time           `json:"lastStartAt"`
	LastStopAt           time.Time           `json:"lastStopAt"`
	Pid                  int                 `json:"pid"`
	StateCode            int8                `json:"stateCode"` // StateCode是如何描述状态的?
	StreamerConnected    bool                `json:"streamerConnected"`
	PlayerIds            UintSlice           `json:"playerIds"`
	PlayerCount          uint                `json:"playerCount"`
	CurrentPak           string              `json:"currentPak"`
	Rendering            bool                `json:"rendering"`
	AutoControl          bool                `json:"autoControl"`
	StopDelay            uint                `json:"stopDelay"`
	AutoResizeRes        bool                `json:"autoResizeRes"`
	Labels               labels.Labels       `json:"labels" gorm:"-"`
	Paks                 []domain.Pak        `json:"paks" gorm:"-"`
	PakName              string              `json:"pakName" gorm:"-"`
	CloudRes             string              `json:"cloudRes"`
	PlayerConfig         domain.PlayerConfig `json:"playerConfig" gorm:"serializer:json"`
}

func (instance *ServerInstance) AfterFind(tx *gorm.DB) (err error) {
	if instance.Metadata != "" {
		var metaData domain.MetaData
		err := yaml.Unmarshal([]byte(instance.Metadata), &metaData)
		if err == nil {
			instance.Labels = labels.Set(metaData.Labels)
		}
	}
	if instance.PaksConfig != "" {
		var paksConfig domain.PakConfig
		err = yaml.Unmarshal([]byte(instance.PaksConfig), &paksConfig)
		if err == nil {
			instance.Paks = paksConfig.Paks
			if instance.CurrentPak != "" {
				for _, pak := range paksConfig.Paks {
					if pak.Value == instance.CurrentPak {
						instance.PakName = pak.Name
					}
				}
			}
		}
	}
	return
}

// 判断server实例是否处于闲置状态，实例未启动为闲置:实例未启动或者实例启动未加载场景
func (instance *ServerInstance) IsIdle() bool {
	if instance.StateCode == 0 {
		return true
	}
	if instance.StateCode == 1 && instance.CurrentPak == "" && instance.PlayerCount <= 0 {
		return true
	}
	return false
}

func (instance *ServerInstance) IsAccessing() bool {
	if instance.StateCode == 1 && instance.PlayerCount > 0 {
		return true
	}
	return false
}


// 判断server实例是否处于使用状态
func (instance *ServerInstance) IsRunning() bool {
	if instance.StateCode == 1 && instance.PlayerCount > 0 {
		return true
	}
	return false
}

// 判断server实例是否处于预备状态
func (instance *ServerInstance) IsReady() bool {
	if instance.StateCode != 1 && instance.AutoControl {
		return true
	}
	return false
}

// 计算serverInstance在被获取时的优先级，获取ticket直接根据优先级选取
func (instance *ServerInstance) GetTicketPriority() int {

	// 启动但未加载场景的实例优先级
	// 未启动且允许自动启动的优先级
	// 启动且已经加载场景的实例（动态切换场景）
	// 共享实例
	// TODO: 默认写法，instance-launcher是无法知道加载的什么场景的
	// if instance.StateCode == 1 && instance
	return -1
}
