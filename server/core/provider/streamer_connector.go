package provider

import (
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
	"sync"
	"thingue-launcher/common/domain"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/message"
	"thingue-launcher/common/provider"
	"thingue-launcher/common/util"
	"time"
)

type StreamerConnector struct {
	SID                 string
	PlayerConnectors    []*PlayerConnector
	playersMu           sync.Mutex // 保护 PlayerConnectors 的增删与快照
	conn                *websocket.Conn
	heartbeatTimer      *time.Timer
	AutoStopTimer       *time.Timer
	EnableRelay         bool
	EnableRenderControl bool
	RenderingState      bool
}

// AddPlayer 加锁追加玩家
func (s *StreamerConnector) AddPlayer(player *PlayerConnector) {
	s.playersMu.Lock()
	defer s.playersMu.Unlock()
	s.PlayerConnectors = append(s.PlayerConnectors, player)
}

// RemovePlayer 加锁移除玩家并通知 streamer；不存在时返回 false（幂等安全）
func (s *StreamerConnector) RemovePlayer(player *PlayerConnector) bool {
	s.playersMu.Lock()
	defer s.playersMu.Unlock()
	for i, p := range s.PlayerConnectors {
		if p == player {
			s.PlayerConnectors = append(s.PlayerConnectors[:i], s.PlayerConnectors[i+1:]...)
			s.SendMessage(util.MapToJson(map[string]any{
				"type":     "playerDisconnected",
				"playerId": player.PlayerId,
			}))
			return true
		}
	}
	return false
}

// Players 加锁快照，遍历请勿直接访问 PlayerConnectors
func (s *StreamerConnector) Players() []*PlayerConnector {
	s.playersMu.Lock()
	defer s.playersMu.Unlock()
	players := make([]*PlayerConnector, len(s.PlayerConnectors))
	copy(players, s.PlayerConnectors)
	return players
}

func (s *StreamerConnector) SendPong(msg map[string]any) {
	s.SendMessage(util.MapToJson(map[string]interface{}{
		"type": "pong",
		"time": msg["time"],
	}))
}

func (s *StreamerConnector) State(msg map[string]any) {
	name := msg["name"].(string)
	value := msg["value"].(bool)
	if "streamingState" == name && !value {
		for _, connector := range s.Players() {
			connector.SendMessage(util.MapToJson(msg))
		}
	}
}

func (s *StreamerConnector) SendMultiuserControl(playerId uint) {
	if _, err := SdpConnProvider.GetPlayer(playerId); err == nil {
		s.SendMessage(util.MapToJson(map[string]any{
			"type":     "multiuserControl",
			"playerId": util.SanitizePlayerId(playerId),
		}))
	} else {
		s.SendMessage(util.MapToJson(map[string]any{
			"type":     "multiuserControl",
			"playerId": "Invalid Player Id",
		}))
	}
}

func (s *StreamerConnector) SendConfig() {
	if s.EnableRelay && provider.AppConfig.PeerConnectionOptions != "" {
		var options domain.PeerConnectionOptions
		err := yaml.Unmarshal([]byte(provider.AppConfig.PeerConnectionOptions), &options)
		if err == nil {
			s.SendMessage(util.MapToJson(map[string]any{
				"type":                  "config",
				"peerConnectionOptions": options,
			}))
			return
		}
	}
	s.SendMessage(util.MapToJson(map[string]any{
		"type":                  "config",
		"peerConnectionOptions": map[string]any{},
	}))
}

func (s *StreamerConnector) ForwardMessage(msg map[string]any) {
	playerId, err := util.GetPlayerIdFromMessage(msg)
	if err != nil {
		s.SendCloseMsg(1008, "不支持的消息类型")
	}
	delete(msg, "playerId")
	for _, player := range s.Players() {
		if player.PlayerId == playerId {
			player.SendMessage(util.MapToJson(msg))
			return
		}
	}
}

func (s *StreamerConnector) PlayerDisconnect(disconnectPlayer *PlayerConnector) {
	s.RemovePlayer(disconnectPlayer)
}

func (s *StreamerConnector) SendPlayersCount() {
	players := s.Players()
	for index, player := range players {
		canResizeRes := false
		if index == 0 {
			canResizeRes = true
		}
		msg := map[string]any{
			"type":         "playerCount",
			"count":        len(players),
			"canResizeRes": canResizeRes,
		}
		player.SendMessage(util.MapToJson(msg))
	}
}

func (s *StreamerConnector) SendMessage(msgStr []byte) {
	s.conn.WriteMessage(websocket.TextMessage, msgStr)
}

func (s *StreamerConnector) SendCloseMsg(code int, msg string) {
	s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, msg))
}

func (s *StreamerConnector) SendCommand(command *message.Command) {
	logger.Zap.Info(string(command.GetBytes()))
	s.conn.WriteMessage(websocket.TextMessage, command.GetBytes())
}

func (s *StreamerConnector) Close() {
	_ = s.conn.Close()
	SdpConnProvider.RemoveStreamer(s.SID)
}

func (s *StreamerConnector) ControlRendering(rendering bool) {
	if s.EnableRenderControl {
		command := message.Command{}
		command.BuildRenderingCommand(&message.RenderingParams{Value: rendering})
		s.SendCommand(&command)
		s.RenderingState = rendering
	}
}

func (s *StreamerConnector) UpdateRenderingState(state bool) {
	s.RenderingState = state
}
