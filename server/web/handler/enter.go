package handler

import (
	"thingue-launcher/server/web/handler/mqtt"
	"thingue-launcher/server/web/handler/rest"
	"thingue-launcher/server/web/handler/ws"
)

var (
	InstanceGroup = new(rest.InstanceGroup)
	SyncGroup     = new(rest.SyncGroup)
	ModelGroup    = new(rest.ModelGroup) // 模型库处理器
	WsGroup       = new(ws.HandlerGroup)
	MqttHandler   = mqtt.MqttHandler
)
