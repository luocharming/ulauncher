package ws

import (
	"github.com/gin-gonic/gin"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/util"
	"thingue-launcher/server/core/provider"
	"thingue-launcher/server/core/service"
	"time"
)

func (g *HandlerGroup) PlayerWebSocketHandler(c *gin.Context) {
	// 被踢 IP 拒绝名单检查（问题1 第二层拦截，覆盖"先拿票后被踢"的窗口）
	if service.DenyService.IsDenied(c.ClientIP()) {
		logger.Zap.Infof("拒绝被踢IP的拉流连接: %s", c.ClientIP())
		c.AbortWithStatus(403)
		return
	}
	conn, err := WsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Zap.Error("WebSocket upgrade error:", err)
		return
	}
	userData := map[string]string{}
	_ = c.ShouldBindQuery(userData)
	player := provider.SdpConnProvider.NewPlayer(conn)
	player.IP = c.ClientIP() // 服务端采集客户端 IP
	player.UserData = userData
	player.StartPingSendTask()
	// 连接Streamer
	err = service.SdpService.ConnectStreamer(player, c.Param("ticket"))
	if err == nil {
		player.SendConfig()
		for {
			// 接收消息
			_, msgStr, err := conn.ReadMessage()
			if err != nil {
				break
			}
			msg := util.JsonStrToMapData(msgStr)
			// 处理不同消息类型
			msgType := msg["type"].(string)
			if msgType == "ping" {
				player.SendPong(msg)
			} else if msgType == "pong" {
				logger.Zap.Debug(msg)
			} else if msgType == "listStreamers" {
				player.ListStreamers()
			} else if msgType == "offer" { // for old streamer
				player.Offer(msg)
				if err := service.SdpService.OnPlayerPaired(player); err != nil {
					break
				}
			} else if msgType == "subscribe" { // for new streamer
				player.Subscribe()
				if err := service.SdpService.OnPlayerPaired(player); err != nil {
					break
				}
			} else if msgType == "answer" { // for new streamer
				player.ForwardMessage(msg)
			} else if msgType == "iceCandidate" {
				player.ForwardMessage(msg)
			} else if msgType == "stats" {
				//todo
			} else if msgType == "kick" {
				player.KickOthers()
			} else {
				player.SendCloseMsg(1008, "不支持的消息类型")
			}
		}
		service.SdpService.OnPlayerDisConnect(player)
	} else {
		// 无法连接Streamer
		time.Sleep(3 * time.Second)
		player.Close()
	}
}
