package request

import (
	"thingue-launcher/common/domain"
)

type PublishJson struct {
	Topic   string         `json:"topic"`
	Payload map[string]any `json:"payload"`
	Retain  bool           `json:"retain"`
	Qos     byte           `json:"qos"`
}

type PublishText struct {
	Topic  string `json:"topic"`
	Text   string `json:"text"`
	Retain bool   `json:"retain"`
	Qos    byte   `json:"qos"`
}

type SelectorCond struct {
	SID               string `json:"sid"`
	Name              string `json:"name"`
	PlayerCount       *int   `json:"playerCount"`
	LabelSelector     string `json:"labelSelector"`
	StreamerConnected bool   `json:"streamerConnected"`
	SceneId           string `json:"sceneId"`
	Shared            *bool  `json:"shared"` // 三态：nil=未携带(旧请求，按共享处理)，true=共享，false=独占
	ClientIP          string `json:"-"`      // 服务端采集（gin c.ClientIP），禁止客户端绑定
}

// KickByIpReq 按 IP 断开实例的拉流客户端
type KickByIpReq struct {
	SID string `json:"sid"`
	IP  string `json:"ip"`
}

// KickAllReq 断开实例的全部拉流客户端
type KickAllReq struct {
	SID  string `json:"sid"`
	Deny *bool  `json:"deny"` // 被踢 IP 是否进拒绝名单；nil/true=进(默认)，false=不进
}

// UpdateInstanceSettingsReq 管理端保存实例分配设置（类型/白名单/连接数上限）
type UpdateInstanceSettingsReq struct {
	SID            string   `json:"sid"`
	InstanceType   int8     `json:"instanceType"`   // 0=共享 1=独占
	Whitelist      []string `json:"whitelist"`      // 白名单 IP；空=不过滤
	MaxPlayerCount int      `json:"maxPlayerCount"` // 共享上限：-1=不限 或 >=1；独占忽略
}

type ClientRegisterInfo struct {
	ClientID   uint
	DeviceInfo *domain.DeviceInfo
	Instances  []*domain.Instance
}

type ProcessControl struct {
	SID     string `json:"sid"`
	Command string `json:"command"`
}

type PakControl struct {
	SID  string `json:"sid"`
	Type string `json:"type"`
	Pak  string `json:"pak"`
}

type LogsCollect struct {
	WsId     int    `json:"wsId"`
	TraceId  string `json:"traceId"`
	ClientId uint   `json:"clientId"`
}
