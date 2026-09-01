package service

import (
	"errors"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/message"
	"thingue-launcher/common/model"
	"thingue-launcher/common/request"
	"thingue-launcher/server/core/provider"
	"time"
)

// 玩家连接失败时回给客户端的 WebSocket 关闭码。
// 客户端据此区分"服务端明确拒绝"与"网络异常"：前者不能立即重连，
// 因为 SDK 的重连会重新申请 ticket，等于换一台实例重新分配、重新拉起。
const (
	ClosePlayerKicked      = 4000 // 被管理员断开（拒绝名单生效期间）
	ClosePlayerTicket      = 4001 // ticket 无效或过期
	ClosePlayerUnavailable = 4002 // 实例不可用：未启动、启动超时、预留失败
)

const (
	// autoStartPollInterval 等待实例自动启动完成的轮询间隔
	autoStartPollInterval = 2 * time.Second
	// autoStartTimeout 自动启动最长等待时间。超时后主动断开，
	// 避免实例始终起不来时 handler 协程与连接无限期挂着（此前是无上限的死等）。
	autoStartTimeout = 60 * time.Second
)

// connectError 携带关闭码的连接失败原因，由 handler 统一发送关闭帧后断开
type connectError struct {
	Code   int
	Reason string
}

func (e *connectError) Error() string { return e.Reason }

// PlayerCloseCode 取出连接失败对应的关闭码与原因；非 connectError 一律按实例不可用处理
func PlayerCloseCode(err error) (int, string) {
	var ce *connectError
	if errors.As(err, &ce) {
		return ce.Code, ce.Reason
	}
	return ClosePlayerUnavailable, err.Error()
}

type sdpService struct{}

var SdpService = sdpService{}

func (m *sdpService) OnStreamerConnect(streamer *provider.StreamerConnector) {
	InstanceService.UpdateStreamerConnected(streamer.SID, true)
	instance := InstanceService.GetInstanceBySid(streamer.SID)
	if instance.SceneId != "" {
		control := request.PakControl{
			SID:  streamer.SID,
			Type: "load",
			Pak:  "Paks/" + instance.SceneId,
		}
		InstanceService.PakControl(control)
	}
	// 开启自动停止任务
	go func() {
		for {
			// todo 释放资源
			<-streamer.AutoStopTimer.C
			getStreamer, err := provider.SdpConnProvider.GetStreamer(streamer.SID)
			if err == nil {
				if streamer != getStreamer {
					logger.Zap.Warn("streamer已重启，自动停止任务关闭")
					break
				}
				if len(getStreamer.Players()) == 0 {
					InstanceService.ProcessControl(request.ProcessControl{
						SID:     getStreamer.SID,
						Command: "STOP",
					})
					logger.Zap.Info("检查完毕，自动停止控制指令发送")
				} else {
					logger.Zap.Debug("检查完毕，不需要自动停止")
				}
			} else {
				logger.Zap.Info("streamer已停止，自动停止任务关闭")
				break
			}
		}
		logger.Zap.Info("自动停止协程结束，资源释放")
	}()
}

func (m *sdpService) OnStreamerDisconnect(streamer *provider.StreamerConnector) {
	for _, playerConnector := range streamer.Players() {
		playerConnector.Close()
	}
	InstanceService.UpdateStreamerConnected(streamer.SID, false)
	streamer.Close()
}

func (m *sdpService) ConnectStreamer(playerConnector *provider.PlayerConnector, ticket string) error {
	if ticket == "test" {
		streamer, err := provider.SdpConnProvider.GetStreamer("test")
		if err != nil {
			return &connectError{Code: ClosePlayerUnavailable, Reason: err.Error()}
		}
		playerConnector.StreamerConnector = streamer
		// 走统一预留机制，配对时可正常 Consume
		ticketId, err := TicketService.Reserve("test", playerConnector.IP, true)
		if err != nil {
			return &connectError{Code: ClosePlayerUnavailable, Reason: err.Error()}
		}
		playerConnector.Ticket = ticketId
		return nil
	}
	sid, err := TicketService.GetSidByTicket(ticket)
	if err != nil {
		return &connectError{Code: ClosePlayerTicket, Reason: "ticket无效或过期"}
	}
	// 共享实例指名直选签发的 ticket（player.html?sid=/?name=、getTicketById 指向共享实例）不参与分配策略，
	// 记录下来供自动启动超时后重新预留放行；独占实例的指名直选已走 Reserve 容量判据，此标记为 false
	playerConnector.Direct = TicketService.IsDirectTicket(ticket)
	// ticket 发出后被踢的窗口：upgrade 之后再次检查拒绝名单（问题1 第二层拦截）
	if DenyService.IsDenied(playerConnector.IP) {
		return &connectError{Code: ClosePlayerKicked, Reason: "kicked"}
	}
	streamer, err := provider.SdpConnProvider.GetStreamer(sid)
	if err == nil {
		playerConnector.StreamerConnector = streamer
		playerConnector.Ticket = ticket
		return nil
	}
	instance := InstanceService.GetInstanceBySid(sid)
	if !instance.AutoControl {
		return &connectError{Code: ClosePlayerUnavailable, Reason: "streamer未连接且未开启自动启动"}
	}
	return m.waitAutoStart(playerConnector, ticket, sid, instance)
}

// waitAutoStart 触发实例自动启动并等待 streamer 上线，等待期间持续为该玩家的 ticket 续期预留。
//
// 续期而不是重新预留是关键：自动启动通常 2~4s 就完成，玩家自己的预留还在 10s TTL 内，
// 此时若调 Reserve 重新预留，这份预留会被算成占用者，独占实例必然返回「实例已被独占占用」
// → 连接失败被断开 → SDK 自动重连 → 重新 ticketSelect → 分到下一台独占实例再拉起，
// 一次点击连锁启动多台实例。续期同时也保证冷启动这几秒里实例不会被别的玩家抢走。
func (m *sdpService) waitAutoStart(playerConnector *provider.PlayerConnector, ticket, sid string, instance *model.ServerInstance) error {
	InstanceService.ProcessControl(request.ProcessControl{
		SID:     sid,
		Command: "START",
	})
	ticker := time.NewTicker(autoStartPollInterval)
	defer ticker.Stop()
	start := time.Now()
	deadline := start.Add(autoStartTimeout)
	reserved := true // 原预留是否仍由本玩家持有
	var streamer *provider.StreamerConnector
	for {
		<-ticker.C
		if reserved {
			reserved = TicketService.Renew(ticket, sid)
		}
		var err error
		if streamer, err = provider.SdpConnProvider.GetStreamer(sid); err == nil {
			break
		}
		if time.Now().After(deadline) {
			logger.Zap.Warnf("自动启动超时 SID=%s 名称=%s IP=%s 已等待=%.0fs",
				sid, instance.Name, playerConnector.IP, time.Since(start).Seconds())
			return &connectError{Code: ClosePlayerUnavailable, Reason: "实例启动超时"}
		}
	}
	playerConnector.StreamerConnector = streamer
	logger.Zap.Infof("自动启动成功 SID=%s 名称=%s IP=%s 耗时=%.1fs 预留续期=%v",
		sid, instance.Name, playerConnector.IP, time.Since(start).Seconds(), reserved)
	if reserved {
		// 预留全程保活，直接沿用原 ticket，配对时正常 Consume
		playerConnector.Ticket = ticket
	} else if playerConnector.Direct {
		// 等待超过 TTL 且预留已被 sweep：共享实例指名直选不做容量判据，重新预留必定成功
		playerConnector.Ticket = TicketService.ReserveDirect(sid, playerConnector.IP)
	} else if ticketId, reserveErr := TicketService.Reserve(sid, playerConnector.IP, instance.InstanceType == 0); reserveErr == nil {
		// 原预留已失效，此时按实例类型重新判容量（独占恒按 1）不会再撞上自己
		playerConnector.Ticket = ticketId
	} else {
		return &connectError{Code: ClosePlayerUnavailable, Reason: reserveErr.Error()}
	}
	// sceneId := TicketService.GetSceneIDBySid(sid)
	if sceneId := instance.SceneId; sceneId != "" {
		InstanceService.PakControl(request.PakControl{
			SID:  sid,
			Type: "load",
			Pak:  "Paks/" + sceneId,
		})
	}
	return nil
}

func (m *sdpService) OnStreamerLoadBundleComplete(streamer *provider.StreamerConnector) {
	if len(streamer.Players()) == 0 {
		streamer.ControlRendering(false)
		InstanceService.UpdateRenderingState(streamer.SID, false)
	}
}

// OnPlayerPaired offer/subscribe 到达时完成配对（ticket 最终消费点）。
// 返回 error 时 handler 应终止读循环；重复调用幂等（paired 标记）。
func (m *sdpService) OnPlayerPaired(player *provider.PlayerConnector) error {
	if player.Paired {
		return nil
	}
	streamer := player.StreamerConnector
	if streamer == nil {
		return errors.New("未连接Streamer")
	}
	// ticket 消费：校验存在/未过期/未消费/归属
	if err := TicketService.Consume(player.Ticket, streamer.SID); err != nil {
		logger.Zap.Infof("玩家配对失败(ticket消费) SID=%s IP=%s err=%v", streamer.SID, player.IP, err)
		player.SendCloseMsg(ClosePlayerTicket, "ticket无效或过期")
		return err
	}
	// 被踢拒绝名单兜底复检（问题1 第三层拦截）
	if DenyService.IsDenied(player.IP) {
		player.SendCloseMsg(ClosePlayerKicked, "kicked")
		return errors.New("已被断开")
	}
	// 独占容量防御性兜底（预留已防，双保险）：独占实例已有玩家则拒绝，指名直选不再放行。
	// 覆盖 Consume 与 UpdatePlayers 落库之间的窗口：预留已消费但玩家数尚未落库时，
	// 后续 Reserve 可能误判为空闲，这里以 streamer 实时的玩家列表为最终判据。
	instance := InstanceService.GetInstanceBySid(streamer.SID)
	if instance.InstanceType == 1 && len(streamer.Players()) >= 1 {
		player.SendCloseMsg(ClosePlayerUnavailable, "实例已被独占占用")
		return errors.New("实例已被独占占用")
	}
	streamer.AddPlayer(player)
	player.Paired = true
	streamer.SendPlayersCount()
	InstanceService.UpdatePlayers(streamer)
	logger.Zap.Infof("玩家配对成功 SID=%s IP=%s 当前玩家数=%d", streamer.SID, player.IP, len(streamer.Players()))
	// 如果未开启渲染，则发消息开启
	if !streamer.RenderingState {
		streamer.ControlRendering(true)
		InstanceService.UpdateRenderingState(streamer.SID, true)
	}
	return nil
}

func (m *sdpService) OnPlayerDisConnect(player *provider.PlayerConnector) {
	player.StreamerConnector.RemovePlayer(player)
	player.StreamerConnector.SendPlayersCount()
	instance := InstanceService.UpdatePlayers(player.StreamerConnector)
	if len(player.StreamerConnector.Players()) == 0 {
		// 关闭渲染
		player.StreamerConnector.ControlRendering(false)
		InstanceService.UpdateRenderingState(player.StreamerConnector.SID, false)
		if instance.AutoControl && instance.StopDelay >= 0 {
			// 启动自动启停延时任务
			player.StreamerConnector.AutoStopTimer.Reset(time.Duration(instance.StopDelay) * time.Second)
		}
	}
	player.Close()
}

// KickPlayerByIp 断开指定实例上指定 IP 的全部玩家，并将该 IP 加入拒绝名单（问题1 规避）
func (m *sdpService) KickPlayerByIp(sid, ip string) (int, error) {
	streamer, err := provider.SdpConnProvider.GetStreamer(sid)
	if err != nil {
		return 0, errors.New("实例未连接Streamer")
	}
	kicked := 0
	for _, p := range streamer.Players() {
		if p.IP == ip {
			m.kickOne(streamer, p)
			kicked++
		}
	}
	if kicked == 0 {
		return 0, errors.New("该IP无已连接玩家")
	}
	DenyService.Add(ip)
	return kicked, nil
}

// KickAllPlayers 断开指定实例的全部玩家；deny=true 时将被踢 IP 加入拒绝名单
func (m *sdpService) KickAllPlayers(sid string, deny bool) (int, error) {
	streamer, err := provider.SdpConnProvider.GetStreamer(sid)
	if err != nil {
		return 0, errors.New("实例未连接Streamer")
	}
	players := streamer.Players()
	for _, p := range players {
		m.kickOne(streamer, p)
		if deny {
			DenyService.Add(p.IP)
		}
	}
	return len(players), nil
}

// kickOne 踢单个玩家：发送关闭消息 + 立即移出切片并通知 streamer + 更新实例计数与广播。
// 随后的 OnPlayerDisConnect 会再次触发，RemovePlayer 查找式删除幂等安全。
func (m *sdpService) kickOne(streamer *provider.StreamerConnector, p *provider.PlayerConnector) {
	p.SendCloseMsg(ClosePlayerKicked, "kicked")
	streamer.RemovePlayer(p)
	streamer.SendPlayersCount()
	InstanceService.UpdatePlayers(streamer)
	p.Close()
}

func (m *sdpService) KickPlayerUser(userQueryMap map[string]string) (int, error) {
	if len(userQueryMap) == 0 {
		return 0, errors.New("参数不能为空")
	}
	players := provider.SdpConnProvider.GetPlayersByUserData(userQueryMap)
	if len(players) > 0 {
		for _, player := range players {
			player.Kick()
		}
		return len(players), nil
	} else {
		return 0, errors.New("没有匹配的连接")
	}
}

func (m *sdpService) OnStreamerNodeRestarted(streamer *provider.StreamerConnector) {
	instance := InstanceService.GetInstanceBySid(streamer.SID)
	restarting := provider.SdpConnProvider.GetStreamerRestartingState(streamer.SID)
	if restarting && instance.CurrentPak != "" {
		logger.Zap.Infof("重启后加载 %s %s", instance.Name, instance.CurrentPak)
		command := message.Command{}
		command.BuildBundleLoadCommand(message.BundleLoadParams{Bundle: instance.CurrentPak})
		streamer.SendCommand(&command)
		provider.SdpConnProvider.SetStreamerRestartingState(streamer.SID, false)
	} else if restarting {
		provider.SdpConnProvider.SetStreamerRestartingState(streamer.SID, false)
		logger.Zap.Infof("重启后不需要加载pak %s %s", instance.Name, instance.CurrentPak)
	} else {
		logger.Zap.Warnf("非重启时忽略nodeRestarted消息 %s", instance.Name)
	}
	if streamer.EnableRenderControl && len(streamer.Players()) == 0 {
		command := message.Command{}
		command.BuildRenderingCommand(&message.RenderingParams{Value: false})
		streamer.SendCommand(&command)
	}
}
