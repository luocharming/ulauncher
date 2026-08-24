package service

import (
	"errors"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/message"
	"thingue-launcher/common/request"
	"thingue-launcher/server/core/provider"
	"time"
)

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
		if err == nil {
			playerConnector.StreamerConnector = streamer
			// 走统一预留机制，配对时可正常 Consume
			if ticketId, reserveErr := TicketService.Reserve("test", playerConnector.IP, true); reserveErr == nil {
				playerConnector.Ticket = ticketId
			} else {
				err = reserveErr
			}
		}
		return err
	}
	sid, err := TicketService.GetSidByTicket(ticket)
	if err != nil {
		playerConnector.SendCloseMsg(4001, "ticket无效或过期")
		return err
	}
	// ticket 发出后被踢的窗口：upgrade 之后再次检查拒绝名单（问题1 第二层拦截）
	if DenyService.IsDenied(playerConnector.IP) {
		playerConnector.SendCloseMsg(4000, "kicked")
		return errors.New("已被断开")
	}
	streamer, err := provider.SdpConnProvider.GetStreamer(sid)
	if err == nil {
		playerConnector.StreamerConnector = streamer
		playerConnector.Ticket = ticket
	} else {
		instance := InstanceService.GetInstanceBySid(sid)
		if instance.AutoControl {
			InstanceService.ProcessControl(request.ProcessControl{
				SID:     sid,
				Command: "START",
			})
			ticker := time.NewTicker(2 * time.Second)
			for {
				<-ticker.C
				streamer, err = provider.SdpConnProvider.GetStreamer(sid)
				if err == nil {
					playerConnector.StreamerConnector = streamer
					ticker.Stop()
					break
				}
			}
			logger.Zap.Info("自动启动成功")
			// 自动启动等待可能超过 ticket 预留的 10s TTL（原预留已随 sweep 失效），
			// 重新预留新 ticket，配对时 Consume 才能通过；容量判定沿用实例类型（独占恒按1）
			if ticketId, reserveErr := TicketService.Reserve(sid, playerConnector.IP, instance.InstanceType == 0); reserveErr == nil {
				playerConnector.Ticket = ticketId
			} else {
				err = reserveErr
			}
			// sceneId := TicketService.GetSceneIDBySid(sid)
			sceneId := instance.SceneId
			if sceneId != "" {
				control := request.PakControl{
					SID:  sid,
					Type: "load",
					Pak:  "Paks/" + sceneId,
				}
				InstanceService.PakControl(control)
			}

		} else {
			err = errors.New("streamer未连接且未开启自动启动")
		}
	}
	return err
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
		player.SendCloseMsg(4001, "ticket无效或过期")
		return err
	}
	// 被踢拒绝名单兜底复检（问题1 第三层拦截）
	if DenyService.IsDenied(player.IP) {
		player.SendCloseMsg(4000, "kicked")
		return errors.New("已被断开")
	}
	// 独占容量防御性兜底（预留已防，双保险）：独占实例已有玩家则拒绝
	instance := InstanceService.GetInstanceBySid(streamer.SID)
	if instance.InstanceType == 1 && len(streamer.Players()) >= 1 {
		player.SendCloseMsg(4001, "实例已被独占占用")
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
	p.SendCloseMsg(4000, "kicked")
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
