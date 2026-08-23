package service

import (
	"errors"
	"fmt"
	"math"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/model"
	"thingue-launcher/common/request"
	"thingue-launcher/common/response"
	"thingue-launcher/server/global"
	"time"

	"github.com/bluele/gcache"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/labels"
)

type ticketService struct {
	cache gcache.Cache
}

var TicketService = ticketService{
	cache: gcache.New(math.MaxInt64).LRU().Build(),
}

func (s *ticketService) GetTicketById(sid string) (string, error) {
	var instance model.ServerInstance
	err := global.SERVER_DB.Where("s_id = ?", sid).First(&instance).Error
	if err == nil {
		ticket, _ := uuid.NewUUID()
		//添加缓存
		err = s.cache.SetWithExpire(ticket.String(), instance.SID, 10*time.Second)
		if err == nil {
			return ticket.String(), err
		}
	}
	return "", err
}

func (s *ticketService) GetSidByTicket(ticket string) (string, error) {
	sid, err := s.cache.Get(ticket)
	if err == nil {
		return sid.(string), nil
	} else {
		return "", errors.New("ticket无效或过期")
	}
}

func (s *ticketService) GetSceneIDBySid(sid string) string {
	sceneId, err := s.cache.Get(sid)
	if err == nil {
		return sceneId.(string)
	} else {
		return ""
	}
}

func (s *ticketService) TicketSelect(selectCond request.SelectorCond) (response.InstanceTicket, error) {
	ticket := response.InstanceTicket{}
	status, err := LicenseService.LoadAndVerify(LicenseService.LicensePath())
	if err != nil || !status.Valid {
		ticket.LicenseApplicationCode = LicenseService.GenerateApplicationCode()
		if err != nil {
			return ticket, err
		}
		if !status.Valid {
			if status.Reason != "" {
				return ticket, errors.New(status.Reason)
			}
			return ticket, errors.New("未授权")
		}
	} else {
		ticket.LicenseExpireDate = status.ExpiresAt.Format("2006-01-02")
		rem := status.ExpiresAt.Sub(time.Now().UTC()).Hours() / 24.0
		if rem < 0 {
			rem = 0
		}
		ticket.LicenseRemainingDays = math.Round(rem*10) / 10
	}
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
	// 判断查询后是否有结果
	if len(findInstances) == 0 {
		return ticket, errors.New("连接数已满")
	}
	// 筛选掉未启动且未开启自动启停的实例
	var readyInstances []*model.ServerInstance
	for _, instance := range findInstances {
		if instance.StateCode == 1 || instance.AutoControl == true {
			readyInstances = append(readyInstances, instance)
		}
	}
	if len(readyInstances) == 0 {
		return ticket, errors.New("实例未启动且未开启自动启停")
	}
	if selectCond.LabelSelector != "" {
		// label匹配
		selector, err := labels.Parse(selectCond.LabelSelector)
		if err != nil {
			return ticket, err // label解析失败
		}

		var selectedInstance []*model.ServerInstance
		for _, instance := range readyInstances {
			if selector.Matches(instance.Labels) {
				selectedInstance = append(selectedInstance, instance)
			}
		}
		if len(selectedInstance) == 0 {
			return ticket, errors.New(fmt.Sprintf("找不到符合%s的可用实例", selectCond.LabelSelector))
		}
		minPlayerCountInstance := selectedInstance[0]
		for _, instance := range selectedInstance {
			if instance.PlayerCount < minPlayerCountInstance.PlayerCount {
				minPlayerCountInstance = instance
			}
		}
		ticketId, _ := uuid.NewUUID()
		s.cache.SetWithExpire(ticketId.String(), minPlayerCountInstance.SID, 10*time.Second)
		ticket.SetInstanceInfo(minPlayerCountInstance)
		ticket.Ticket = ticketId.String()
		return ticket, nil
	} else {
		minPlayerCountInstance := readyInstances[0]
		for _, instance := range readyInstances {
			if instance.PlayerCount < minPlayerCountInstance.PlayerCount {
				minPlayerCountInstance = instance
			}
		}
		//不需要label匹配，挑选第一个生成ticket
		ticketId, _ := uuid.NewUUID()
		//添加缓存
		s.cache.SetWithExpire(ticketId.String(), minPlayerCountInstance.SID, 10*time.Second)
		ticket.SetInstanceInfo(minPlayerCountInstance)
		ticket.Ticket = ticketId.String()
		return ticket, nil
	}
}

func (s *ticketService) TicketSelect2(selectCond request.SelectorCond) (response.InstanceTicket, error) {
	ticket := response.InstanceTicket{}
	status, err := LicenseService.LoadAndVerify(LicenseService.LicensePath())
	if err != nil || !status.Valid {
		ticket.LicenseApplicationCode = LicenseService.GenerateApplicationCode()
		if err != nil {
			return ticket, err
		}
		if !status.Valid {
			if status.Reason != "" {
				return ticket, errors.New(status.Reason)
			}
			return ticket, errors.New("未授权")
		}
	} else {
		ticket.LicenseExpireDate = status.ExpiresAt.Format("2006-01-02")
		rem := status.ExpiresAt.Sub(time.Now().UTC()).Hours() / 24.0
		if rem < 0 {
			rem = 0
		}
		ticket.LicenseRemainingDays = math.Round(rem*10) / 10
	}
	query := global.SERVER_DB
	if selectCond.StreamerConnected == true {
		query = query.Where("streamer_connected = ?", selectCond.StreamerConnected)
	}
	if selectCond.SID != "" {
		query = query.Where("s_id = ?", selectCond.SID)
		var instances []*model.ServerInstance
		query.Find(&instances)
		if len(instances) == 0 {
			return ticket, errors.New(fmt.Sprintf("未查找到sid:%s的实例", selectCond.SID))
		}
	}
	if selectCond.Name != "" {
		query = query.Where("name = ?", selectCond.Name)
	}
	if selectCond.PlayerCount != nil && *selectCond.PlayerCount >= 0 {
		query = query.Where("player_count < ?", selectCond.PlayerCount)
	}

	var findInstances []*model.ServerInstance
	query.Find(&findInstances)
	if len(findInstances) == 0 {
		return ticket, errors.New("连接数已满")
	}

	// 筛选掉未启动且未开启自动启停的实例
	var readyInstances []*model.ServerInstance
	for _, instance := range findInstances {
		if instance.StateCode == 1 || instance.AutoControl == true {
			readyInstances = append(readyInstances, instance)
		}
	}
	if len(readyInstances) == 0 {
		return ticket, errors.New("实例未启动且未开启自动启停")
	}

	if selectCond.SID != "" {
		//通过sid访问的情况下不走元数据的方式，直接返回sid的实例
		for _, instance := range findInstances {
			if instance.SID == selectCond.SID {
				ticketId, _ := uuid.NewUUID()
				s.cache.SetWithExpire(ticketId.String(), instance.SID, 10*time.Second)
				ticket.SetInstanceInfo(instance)
				ticket.Ticket = ticketId.String()
				return ticket, nil
			}
		}
	}

	sceneId := selectCond.SceneId
	enableShared := selectCond.Shared

	// 携带元数据
	if sceneId != "" {
		//优先级1：寻找匹配实例：从已开启实例中寻找加载了当前场景的匹配实例
		var hasMatchedPak = false
		// 匹配元数据匹配的实例
		for _, instance := range findInstances {
			if instance.CurrentPak == sceneId && instance.StateCode == 1 {
				hasMatchedPak = true
				instance.SceneId = sceneId
				InstanceService.UpdateInstance(instance)
				ticketId, _ := uuid.NewUUID()
				s.cache.SetWithExpire(ticketId.String(), instance.SID, 10*time.Second)
				ticket.SetInstanceInfo(instance)
				ticket.Ticket = ticketId.String()
				logger.Zap.Infof("【携带元数据】发现当前场景是元数据的匹配实例")
				return ticket, nil
			}
		}
		//如果没有匹配实例，则查找空闲实例
		if !hasMatchedPak {
			// 优先级2：寻找空闲实例：即未开启的实例或开启后未加载场景的实例
			var hasIdleInstance = false
			for _, instance := range findInstances {
				if instance.IsIdle() {
					hasIdleInstance = true
					instance.SceneId = sceneId
					InstanceService.UpdateInstance(instance)
					ticketId, _ := uuid.NewUUID()
					s.cache.SetWithExpire(ticketId.String(), instance.SID, 10*time.Second)
					s.cache.SetWithExpire(instance.SID, sceneId, 10*time.Second)
					ticket.SetInstanceInfo(instance)
					ticket.Ticket = ticketId.String()
					logger.Zap.Infof("【携带元数据】:发现空闲实例，即将加载场景")
					return ticket, nil
				}
			}
			// 如果没有空闲实例则查找预备实例
			if !hasIdleInstance {
				// 获取预备实例，即连接数为0的实例
				var hasNonAccessingInstance = false
				for _, instance := range findInstances {
					if !instance.IsAccessing() {
						hasNonAccessingInstance = true
						instance.SceneId = sceneId
						InstanceService.UpdateInstance(instance)
						ticketId, _ := uuid.NewUUID()
						s.cache.SetWithExpire(ticketId.String(), instance.SID, 10*time.Second)
						ticket.SetInstanceInfo(instance)
						ticket.Ticket = ticketId.String()
						logger.Zap.Infof("【携带元数据】获取连接数为0的预备实例,切换场景")
						s.LoadBundle(instance.SID, instance.SceneId)
						return ticket, nil
					}
				}
				// 没有预备实例，则查找共享实例
				if !hasNonAccessingInstance {
					//获取共享实例，即在设置中开启共享实例的实例
					for _, instance := range findInstances {
						if instance.EnableSharedInstance && enableShared {
							instance.SceneId = sceneId
							InstanceService.UpdateInstance(instance)
							ticketId, _ := uuid.NewUUID()
							s.cache.SetWithExpire(ticketId.String(), instance.SID, 10*time.Second)
							ticket.SetInstanceInfo(instance)
							ticket.Ticket = ticketId.String()
							logger.Zap.Infof("【携带元数据】使用共享实例")
							s.LoadBundle(instance.SID, instance.SceneId)
							return ticket, nil
						}
					}
					return ticket, errors.New("未找到空闲实例，需要开启共享实例")
				}
			}
		}

	}

	if selectCond.LabelSelector != "" {
		// label匹配
		selector, err := labels.Parse(selectCond.LabelSelector)
		if err != nil {
			return ticket, err // label解析失败
		}

		var selectedInstance []*model.ServerInstance
		for _, instance := range readyInstances {
			if selector.Matches(instance.Labels) {
				selectedInstance = append(selectedInstance, instance)
			}
		}
		if len(selectedInstance) == 0 {
			return ticket, errors.New(fmt.Sprintf("找不到符合%s的可用实例", selectCond.LabelSelector))
		}
		minPlayerCountInstance := selectedInstance[0]
		for _, instance := range selectedInstance {
			if instance.PlayerCount < minPlayerCountInstance.PlayerCount {
				minPlayerCountInstance = instance
			}
		}
		ticketId, _ := uuid.NewUUID()
		s.cache.SetWithExpire(ticketId.String(), minPlayerCountInstance.SID, 10*time.Second)
		ticket.SetInstanceInfo(minPlayerCountInstance)
		ticket.Ticket = ticketId.String()
		return ticket, nil
	}

	// 查找是否有空闲实例，如果有则返回空闲实例
	var hasIdleInstance = false
	for _, instance := range findInstances {
		if instance.IsIdle() {
			hasIdleInstance = true
			ticketId, _ := uuid.NewUUID()
			s.cache.SetWithExpire(ticketId.String(), instance.SID, 10*time.Second)
			ticket.SetInstanceInfo(instance)
			ticket.Ticket = ticketId.String()
			logger.Zap.Infof("【未携带元数据】发现空闲实例")
			return ticket, nil
		}
	}

	if !hasIdleInstance {
		instance := findInstances[0]
		ticketId, _ := uuid.NewUUID()
		s.cache.SetWithExpire(ticketId.String(), instance.SID, 10*time.Second)
		ticket.SetInstanceInfo(instance)
		ticket.Ticket = ticketId.String()
		logger.Zap.Infof("【未携带元数据】默认取第一个实例")
		//未携带元数据默认获取第一个实例
		return ticket, nil
	}

	return ticket, errors.New("未找到可用实例")
}

func (s *ticketService) LoadBundle(sid string, pak string) error {
	control := request.PakControl{
		SID:  sid,
		Type: "load",
		Pak:  "Paks/" + pak,
	}
	return InstanceService.PakControl(control)
}
