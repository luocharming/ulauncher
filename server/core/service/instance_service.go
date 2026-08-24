package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/message"
	"thingue-launcher/common/model"
	"thingue-launcher/common/request"
	"thingue-launcher/common/util"
	"thingue-launcher/server/core/provider"
	"thingue-launcher/server/global"
	"time"

	"gorm.io/gorm/clause"
	"k8s.io/apimachinery/pkg/labels"
)

type instanceService struct {
	updateLock sync.Mutex
}

var InstanceService = new(instanceService)

func (s *instanceService) GetInstanceBySid(sid string) *model.ServerInstance {
	instance := &model.ServerInstance{}
	global.SERVER_DB.Where("s_id = ?", sid).First(instance)
	return instance
}

func (s *instanceService) UpdatePlayers(streamer *provider.StreamerConnector) *model.ServerInstance {
	s.updateLock.Lock()
	defer s.updateLock.Unlock()
	instance := s.GetInstanceBySid(streamer.SID)
	players := streamer.Players() // 加锁快照
	playerIds := make(model.UintSlice, 0, len(players))
	playerIps := make(model.StringSlice, 0, len(players))
	for _, connector := range players {
		playerIds = append(playerIds, connector.PlayerId)
		playerIps = append(playerIps, connector.IP)
	}
	instance.PlayerIds = playerIds
	instance.PlayerIps = playerIps // 与 PlayerIds 同源同快照，原子一致；权威数据仍在 PlayerConnectors
	instance.PlayerCount = uint(len(players))
	if instance.SceneId != "" && instance.CurrentPak == "" {
		instance.CurrentPak = instance.SceneId
	}
	if err := global.SERVER_DB.Save(&instance).Error; err != nil {
		logger.Zap.Errorf("实例玩家状态写入失败 SID=%s err=%v", streamer.SID, err)
	}
	logger.Zap.Infof("实例玩家更新 SID=%s 玩家数=%d IPs=%v", streamer.SID, len(players), playerIps)
	provider.AdminConnProvider.BroadcastUpdate()
	return instance
}

func (s *instanceService) UpdateInstance(instance *model.ServerInstance) {
	s.updateLock.Lock()
	defer s.updateLock.Unlock()
	global.SERVER_DB.Save(&instance)
}

// UpdateInstanceSettings 管理端保存实例分配设置（类型/白名单/连接数上限）：
// 校验 → 写 SERVER_DB → upsert STORAGE_DB（last-write-wins）→ BroadcastUpdate
func (s *instanceService) UpdateInstanceSettings(req request.UpdateInstanceSettingsReq) (*model.ServerInstance, error) {
	s.updateLock.Lock()
	defer s.updateLock.Unlock()
	instance := &model.ServerInstance{}
	if err := global.SERVER_DB.Where("s_id = ?", req.SID).First(instance).Error; err != nil {
		return nil, errors.New("未找到该实例")
	}
	if req.InstanceType != 0 && req.InstanceType != 1 {
		return nil, errors.New("实例类型非法")
	}
	// 白名单逐条强校验：精确 IP（IPv4/IPv6）或 IPv4 通配网段（192.168.1.*），存储规范化格式并去重；
	// 空行（前端新增未填写的行）直接忽略
	normalized := make(model.StringSlice, 0, len(req.Whitelist))
	for _, rule := range req.Whitelist {
		trimmed := strings.TrimSpace(rule)
		if trimmed == "" {
			continue
		}
		parsed := util.NormalizeIPRule(trimmed)
		if parsed == "" {
			return nil, fmt.Errorf("白名单包含非法IP或网段: %s", trimmed)
		}
		if util.ContainsString(normalized, parsed) {
			continue
		}
		normalized = append(normalized, parsed)
	}
	// 连接数上限仅 -1(不限) 或 >=1；0 与其它负数非法
	if req.MaxPlayerCount < -1 || req.MaxPlayerCount == 0 {
		return nil, errors.New("连接数上限仅支持-1(不限)或>=1")
	}
	// 共享→独占切换需当前连接数<=1（独占实例连接数恒为1）
	if req.InstanceType == 1 && instance.InstanceType == 0 && instance.PlayerCount > 1 {
		return nil, errors.New("当前连接数超过1，请先断开后再切换为独占")
	}
	// 先 upsert STORAGE_DB：持久化行才是权威来源（SERVER_DB 是内存库，register 时按 SID 合并回来），
	// 写失败必须直接报错，否则管理端显示"已保存"、客户端一重连设置就被旧值覆盖。
	// 冲突目标必须显式指定 s_id：GORM 默认按主键 id 生成 ON CONFLICT，而新建行 id 为零值永不冲突，
	// 实际会撞上 s_id 唯一索引 → 同一实例的第二次及以后保存全部失败。
	settings := model.InstanceSettings{
		SID:            req.SID,
		InstanceType:   req.InstanceType,
		Whitelist:      normalized,
		MaxPlayerCount: req.MaxPlayerCount,
		LastSeenAt:     time.Now(),
	}
	if err := global.STORAGE_DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "s_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"instance_type", "whitelist", "max_player_count", "last_seen_at", "updated_at",
		}),
	}).Create(&settings).Error; err != nil {
		logger.Zap.Errorf("实例设置持久化失败 SID=%s err=%v", req.SID, err)
		return nil, errors.New("设置持久化失败，请重试")
	}
	// 再写 SERVER_DB（运行时状态，分配逻辑读这里）
	if err := global.SERVER_DB.Model(&model.ServerInstance{}).Where("s_id = ?", req.SID).Updates(map[string]any{
		"instance_type":    req.InstanceType,
		"whitelist":        normalized,
		"max_player_count": req.MaxPlayerCount,
	}).Error; err != nil {
		logger.Zap.Errorf("实例设置写入运行时库失败 SID=%s err=%v", req.SID, err)
		return nil, errors.New("设置写入失败，请重试")
	}
	logger.Zap.Infof("实例设置已保存 SID=%s 类型=%d 白名单=%v 上限=%d",
		req.SID, req.InstanceType, normalized, req.MaxPlayerCount)
	provider.AdminConnProvider.BroadcastUpdate()
	return s.GetInstanceBySid(req.SID), nil
}

func (s *instanceService) UpdateStreamerConnected(sid string, connected bool) {
	s.updateLock.Lock()
	defer s.updateLock.Unlock()
	global.SERVER_DB.Model(&model.ServerInstance{}).Where("s_id = ?", sid).Update("streamer_connected", connected)
	provider.AdminConnProvider.BroadcastUpdate()
	instance := model.ServerInstance{}
	global.SERVER_DB.Where("s_id = ?", sid).First(&instance)
	updateMsg := message.ServerStreamerConnectedUpdate{
		CID:       instance.CID,
		Connected: connected,
	}
	provider.ClientConnProvider.SendToClient(instance.ClientID, updateMsg.Pack())
}

func (s *instanceService) UpdateProcessState(msg *message.ClientProcessStateUpdate) {
	global.SERVER_DB.Model(&model.ServerInstance{}).Where("s_id = ?", msg.SID).Updates(map[string]any{"Pid": msg.Pid, "StateCode": msg.StateCode})
	provider.AdminConnProvider.BroadcastUpdate()
}

func (s *instanceService) UpdateRenderingState(sid string, rendering bool) {
	global.SERVER_DB.Model(&model.ServerInstance{}).Where("s_id = ?", sid).Update("rendering", rendering)
}

func (s *instanceService) UpdatePak(sid, currentPakValue string) {
	if currentPakValue != "" {
		instance := model.ServerInstance{}
		global.SERVER_DB.Where("s_id = ?", sid).First(&instance)
		found := false
		for _, pak := range instance.Paks {
			if pak.Value == currentPakValue {
				found = true
				break
			}
		}
		if !found {
			logger.Zap.Debug("未配置的pak值", currentPakValue)
			return
		}
	}
	global.SERVER_DB.Model(&model.ServerInstance{}).Where("s_id = ?", sid).Update("current_pak", currentPakValue)
	provider.AdminConnProvider.BroadcastUpdate()
}

func (s *instanceService) ProcessControl(processControl request.ProcessControl) {
	var instance model.ServerInstance
	global.SERVER_DB.Where("s_id = ?", processControl.SID).First(&instance)
	control := message.ServerProcessControl{
		CID:     instance.CID,
		Command: processControl.Command,
	}
	provider.ClientConnProvider.SendToClient(instance.ClientID, control.Pack())
	if processControl.Command == "STOP" {
		s.UpdatePak(instance.SID, "")
	}

}

func (s *instanceService) PakControl(control request.PakControl) error {
	command := message.Command{}
	instance := s.GetInstanceBySid(control.SID)
	if control.Type == "load" {
		//如果没有获取到instance，就不加载场景
		if instance.SID == control.SID {
			if instance.CurrentPak != control.Pak {
				command.BuildBundleLoadCommand(message.BundleLoadParams{Bundle: control.Pak})
				pakName := control.Pak
				index := strings.Index(pakName, "/")
				instance.CurrentPak = pakName[index+1:]
				instance.SceneId = instance.CurrentPak
				s.UpdateInstance(instance)
			} else {
				return errors.New("已经加载当前Pak")
			}
		}

	} else if control.Type == "unload" {
		if instance.SID == control.SID {
			command.BuildBundleUnloadCommand()
			instance.CurrentPak = ""
			instance.SceneId = ""
			s.UpdateInstance(instance)
		}
	} else {
		return errors.New("不支持的消息类型")
	}
	streamer, err := provider.SdpConnProvider.GetStreamer(control.SID)
	if err == nil {
		streamer.SendCommand(&command)
	}
	return err
}

func (s *instanceService) InstanceList() ([]*model.ServerInstance, error) {
	var instances []*model.ServerInstance
	global.SERVER_DB.Find(&instances)
	return instances, nil
}

func (s *instanceService) InstanceSelect(selectCond request.SelectorCond) ([]*model.ServerInstance, error) {
	// 数据库查询
	//query := global.SERVER_DB.Where("state_code = ? or auto_control = ?", 1, true)
	query := global.SERVER_DB
	if selectCond.StreamerConnected == true {
		query = query.Where("streamer_connected = ?", selectCond.StreamerConnected)
	}
	if selectCond.SID != "" {
		query = query.Where("s_id = ?", selectCond.SID)
	}
	if selectCond.Name != "" {
		query = query.Where("name = ?", selectCond.Name)
	}
	if selectCond.PlayerCount != nil && *selectCond.PlayerCount >= 0 {
		query = query.Where("player_count < ?", selectCond.PlayerCount)
	}
	var findInstances []*model.ServerInstance
	query.Find(&findInstances)
	// 筛选掉未启动且未开启自动启停的实例
	var readyInstances []*model.ServerInstance
	for _, instance := range findInstances {
		if instance.StateCode == 1 || instance.AutoControl == true {
			readyInstances = append(readyInstances, instance)
		}
	}
	if len(readyInstances) > 0 && selectCond.LabelSelector != "" {
		// label匹配
		selector, err := labels.Parse(selectCond.LabelSelector)
		if err != nil {
			return nil, err // label解析失败
		}
		var matchInstances []*model.ServerInstance
		for _, instance := range readyInstances {
			if selector.Matches(instance.Labels) {
				matchInstances = append(matchInstances, instance)
			}
		}
		return matchInstances, nil
	} else {
		return readyInstances, nil
	}
}

func (s *instanceService) GetInstanceByHostnameAndPid(hostname string, pid int) (*model.ServerInstance, error) {
	db := global.SERVER_DB
	instance := &model.ServerInstance{}
	tx := db.Debug().Select("server_instances.*").Joins("JOIN clients ON server_instances.client_id=clients.id AND clients.hostname = ? AND server_instances.pid = ?",
		hostname, pid).First(instance)
	if tx.Error == nil {
		return instance, nil
	} else {
		return nil, tx.Error
	}
}
