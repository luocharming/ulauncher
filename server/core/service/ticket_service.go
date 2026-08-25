package service

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/model"
	"thingue-launcher/common/request"
	"thingue-launcher/common/response"
	"thingue-launcher/common/util"
	"thingue-launcher/server/global"
	"time"

	"github.com/bluele/gcache"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/labels"
)

// ticketExpire ticket 与预留的过期时间（两者同 TTL，见 sweepLocked）
const ticketExpire = 10 * time.Second

type ticketReservation struct {
	sid      string
	ip       string
	expireAt time.Time
	consumed bool
}

type ticketService struct {
	cache        gcache.Cache
	resMu        sync.Mutex                    // 预留锁：容量判断+预留计数在同一临界区内完成
	reservations map[string]*ticketReservation // ticket → 预留
	sidReserved  map[string]uint               // sid → 未消费预留数
}

var TicketService = ticketService{
	cache:        gcache.New(math.MaxInt64).LRU().Build(),
	reservations: make(map[string]*ticketReservation),
	sidReserved:  make(map[string]uint),
}

// sweepLocked 惰性清理过期预留：预留与 ticket 缓存同 10s TTL，
// 同步移除 ticket 缓存，杜绝"预留没了票还在"，并堵住"同 ticket 10 秒内复用"漏洞。
// 调用方需已持有 resMu。
func (s *ticketService) sweepLocked(now time.Time) {
	for t, r := range s.reservations {
		if now.After(r.expireAt) {
			delete(s.reservations, t)
			if s.sidReserved[r.sid] > 0 {
				s.sidReserved[r.sid]--
			}
			s.cache.Remove(t)
		}
	}
}

// Reserve 原子"判断容量 + 预留计数"，成功返回 ticket。
// shared=true 按共享容量判据（MaxPlayerCount<=0 永不判满），false 按独占判据（恒 1）。
// 锁序约定：resMu → (释放后) → streamer.playersMu → (释放后) → instanceService.updateLock，从不嵌套。
func (s *ticketService) Reserve(sid, ip string, shared bool) (string, error) {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	s.sweepLocked(time.Now())
	instance := InstanceService.GetInstanceBySid(sid)
	if shared {
		if instance.MaxPlayerCount > 0 &&
			int(instance.PlayerCount)+int(s.sidReserved[sid]) >= instance.MaxPlayerCount {
			return "", errors.New("共享实例连接数已满")
		}
	} else {
		if int(instance.PlayerCount)+int(s.sidReserved[sid]) >= 1 {
			return "", errors.New("实例已被独占占用")
		}
	}
	ticket, _ := uuid.NewUUID()
	s.reservations[ticket.String()] = &ticketReservation{
		sid:      sid,
		ip:       ip,
		expireAt: time.Now().Add(ticketExpire),
	}
	s.sidReserved[sid]++
	s.cache.SetWithExpire(ticket.String(), sid, ticketExpire)
	return ticket.String(), nil
}

// Consume 配对时消费预留：校验存在/未过期/未消费/归属 → 扣减并删除 ticket 缓存（同 ticket 不可复用）。
func (s *ticketService) Consume(ticket, sid string) error {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	s.sweepLocked(time.Now())
	r, ok := s.reservations[ticket]
	if !ok || r.consumed || r.sid != sid {
		return errors.New("ticket无效或过期")
	}
	r.consumed = true
	if s.sidReserved[sid] > 0 {
		s.sidReserved[sid]--
	}
	delete(s.reservations, ticket)
	s.cache.Remove(ticket)
	return nil
}

func (s *ticketService) GetTicketById(sid string, clientIP string) (string, error) {
	var instance model.ServerInstance
	err := global.SERVER_DB.Where("s_id = ?", sid).First(&instance).Error
	if err != nil {
		return "", err
	}
	// 与 TicketSelect2 的 SID 直选一致：白名单准入优先于容量校验，避免绕过白名单直接取票
	if !whitelistAllows(&instance, clientIP) {
		logger.Zap.Warnf("getTicketById 被白名单拒绝 SID=%s IP=%s 白名单=%v", sid, clientIP, instance.Whitelist)
		return "", errors.New("当前IP不在该实例白名单内")
	}
	// 与 TicketSelect2 同一预留机制，容量校验一致
	return s.Reserve(sid, clientIP, instance.InstanceType == 0)
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
	// 1) License 校验（现状保留）
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
	// 2) 被踢 IP 拒绝名单检查（问题1 第一层拦截，handler 侧转 403）
	if DenyService.IsDenied(selectCond.ClientIP) {
		logger.Zap.Warnf("【分配】拒绝名单拦截 clientIP=%s，直接拒绝出票", selectCond.ClientIP)
		return ticket, ErrKickedDenied
	}
	return s.selectInstance(selectCond, ticket)
}

// selectInstance 实例分配主流程（License / 拒绝名单校验之后的全部选择逻辑）。
// 独立成函数便于单测：可绕过 License 直接对内存库跑需求 5.1/5.2 的分配场景。
func (s *ticketService) selectInstance(selectCond request.SelectorCond, ticket response.InstanceTicket) (response.InstanceTicket, error) {
	// 3) 请求类型三态：nil/true=共享，false=独占
	isSharedReq := selectCond.Shared == nil || *selectCond.Shared
	typeLabel := "共享(旧请求未携带shared)"
	if selectCond.Shared != nil {
		if *selectCond.Shared {
			typeLabel = "共享"
		} else {
			typeLabel = "独占"
		}
	}
	playerCountText := "nil"
	if selectCond.PlayerCount != nil {
		playerCountText = fmt.Sprintf("%d", *selectCond.PlayerCount)
	}
	logger.Zap.Infof("【分配】开始选择实例 请求类型=%s clientIP=%s sceneId=%q sid=%q name=%q playerCount=%s labelSelector=%q streamerConnected=%v",
		typeLabel, selectCond.ClientIP, selectCond.SceneId, selectCond.SID, selectCond.Name,
		playerCountText, selectCond.LabelSelector, selectCond.StreamerConnected)
	// 4) 数据库过滤 + 顺序（c_id 为客户端本地自增主键即实例配置顺序；跨客户端按客户端顺序）
	query := global.SERVER_DB.Order("client_id asc, c_id asc")
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
	logger.Zap.Infof("【分配】数据库查询命中 %d 个实例", len(findInstances))
	for _, instance := range findInstances {
		logger.Zap.Infof("【分配】db=%s", instanceBrief(instance, selectCond.ClientIP))
	}
	if len(findInstances) == 0 {
		return ticket, errors.New("连接数已满")
	}
	// 5) ready 过滤（现状保留：未启动且未开启自动启停的实例不可分配）
	var readyInstances []*model.ServerInstance
	for _, instance := range findInstances {
		if instance.StateCode == 1 || instance.AutoControl == true {
			readyInstances = append(readyInstances, instance)
		} else {
			logger.Zap.Infof("【分配】ready过滤剔除 SID=%s 名称=%s 原因=未启动且未开启自动启停(状态=%d 自动启停=%v)",
				instance.SID, instance.Name, instance.StateCode, instance.AutoControl)
		}
	}
	if len(readyInstances) == 0 {
		return ticket, errors.New("实例未启动且未开启自动启停")
	}
	// 6) SID 直选：跳过选择策略，但仍做白名单准入（player.html?sid= 直连不能绕过白名单）与容量校验、预留。
	//    容量判据按实例自身类型（与 GetTicketById 一致）：独占实例恒按 1，请求 shared 字段不能放宽容量。
	if selectCond.SID != "" {
		for _, instance := range findInstances {
			if instance.SID == selectCond.SID {
				if !whitelistAllows(instance, selectCond.ClientIP) {
					logger.Zap.Warnf("【分配】SID直选被白名单拒绝 SID=%s 名称=%s 白名单=%v clientIP=%s 逐条匹配=%s",
						instance.SID, instance.Name, instance.Whitelist, selectCond.ClientIP,
						ruleMatchBrief(instance.Whitelist, selectCond.ClientIP))
					return ticket, errors.New("当前IP不在该实例白名单内")
				}
				ticketId, err := s.Reserve(instance.SID, selectCond.ClientIP, instance.InstanceType == 0)
				if err != nil {
					logger.Zap.Warnf("【分配】SID直选预留失败 SID=%s 名称=%s err=%v", instance.SID, instance.Name, err)
					return ticket, err
				}
				ticket.SetInstanceInfo(instance)
				ticket.Ticket = ticketId
				logger.Zap.Infof("【分配】SID直选命中 SID=%s 名称=%s 类型=%d 白名单=%v，已签发ticket",
					instance.SID, instance.Name, instance.InstanceType, instance.Whitelist)
				return ticket, nil
			}
		}
		return ticket, errors.New(fmt.Sprintf("未查找到sid:%s的实例", selectCond.SID))
	}
	// 7) 类型池过滤（nil 与 true 都进共享池；false 进独占池；池间不交叉回退）
	//    + 白名单准入过滤：配置了白名单但当前 IP 未命中的实例，两段都不参与（严格准入）
	var candidates []*model.ServerInstance
	deniedByWhitelist := 0
	for _, instance := range readyInstances {
		if isSharedReq && instance.InstanceType != 0 {
			logger.Zap.Infof("【分配】类型池剔除 SID=%s 名称=%s 实例类型=独占 请求类型=%s", instance.SID, instance.Name, typeLabel)
			continue
		}
		if !isSharedReq && instance.InstanceType != 1 {
			logger.Zap.Infof("【分配】类型池剔除 SID=%s 名称=%s 实例类型=共享 请求类型=%s", instance.SID, instance.Name, typeLabel)
			continue
		}
		if !whitelistAllows(instance, selectCond.ClientIP) {
			deniedByWhitelist++
			logger.Zap.Infof("【分配】白名单准入拒绝 SID=%s 名称=%s 白名单=%v clientIP=%s 逐条匹配=%s（该实例不参与本次分配）",
				instance.SID, instance.Name, instance.Whitelist, selectCond.ClientIP,
				ruleMatchBrief(instance.Whitelist, selectCond.ClientIP))
			continue
		}
		candidates = append(candidates, instance)
	}
	if len(candidates) == 0 {
		if deniedByWhitelist > 0 {
			logger.Zap.Warnf("【分配】无候选实例：%d 个实例被白名单准入拒绝 clientIP=%s", deniedByWhitelist, selectCond.ClientIP)
			return ticket, errors.New("当前IP不在可用实例的白名单内")
		}
		if isSharedReq {
			return ticket, errors.New("没有可用的共享实例")
		}
		return ticket, errors.New("没有可用的独占实例")
	}
	logger.Zap.Infof("【分配】候选实例 %d 个（白名单准入拒绝 %d 个）", len(candidates), deniedByWhitelist)
	for _, instance := range candidates {
		logger.Zap.Infof("【分配】候选=%s", instanceBrief(instance, selectCond.ClientIP))
	}
	// 8) labelSelector 过滤（现状保留，作为候选过滤器；匹配后保持 c_id 顺序）
	if selectCond.LabelSelector != "" {
		selector, err := labels.Parse(selectCond.LabelSelector)
		if err != nil {
			return ticket, err
		}
		var matched []*model.ServerInstance
		for _, instance := range candidates {
			if selector.Matches(instance.Labels) {
				matched = append(matched, instance)
			}
		}
		if len(matched) == 0 {
			return ticket, errors.New(fmt.Sprintf("找不到符合%s的可用实例", selectCond.LabelSelector))
		}
		logger.Zap.Infof("【分配】labelSelector=%q 过滤后剩余 %d 个候选", selectCond.LabelSelector, len(matched))
		candidates = matched
	}
	// 9) 分配：携带 sceneId 走场景四级优先级；否则严格按顺序（需求 5.1/5.2 两段式）
	if selectCond.SceneId != "" {
		return s.selectWithScene(candidates, selectCond, isSharedReq, ticket)
	}
	for _, pass := range []bool{true, false} { // true=白名单命中段, false=无白名单段
		passName := "无白名单段"
		if pass {
			passName = "白名单命中段"
		}
		logger.Zap.Infof("【分配】进入%s", passName)
		for _, instance := range candidates {
			if pass != whitelistHit(instance, selectCond.ClientIP) {
				continue
			}
			ticketId, err := s.Reserve(instance.SID, selectCond.ClientIP, isSharedReq)
			if err != nil {
				logger.Zap.Infof("【分配】[%s] SID=%s 名称=%s 预留失败: %v（继续下一个）", passName, instance.SID, instance.Name, err)
				continue
			}
			ticket.SetInstanceInfo(instance)
			ticket.Ticket = ticketId
			logger.Zap.Infof("【分配】[%s] 选中实例 SID=%s 名称=%s 玩家数=%d，已签发ticket（未携带场景，按顺序分配）",
				passName, instance.SID, instance.Name, instance.PlayerCount)
			return ticket, nil
		}
	}
	// 10) 两段全部落空
	if isSharedReq {
		logger.Zap.Warnf("【分配】两段落空：共享实例均不可分配 clientIP=%s", selectCond.ClientIP)
		return ticket, errors.New("共享实例连接数已满")
	}
	logger.Zap.Warnf("【分配】两段落空：独占实例均不可分配 clientIP=%s", selectCond.ClientIP)
	return ticket, errors.New("独占实例已全部被占用")
}

// selectWithScene 携带 sceneId 时的分配：保留现有场景四级优先级（级内 c_id 升序），
// 每级内叠加两段式白名单遍历（白名单命中段 → 无白名单段），容量由 Reserve 原子校验。
func (s *ticketService) selectWithScene(candidates []*model.ServerInstance, cond request.SelectorCond, isSharedReq bool, ticket response.InstanceTicket) (response.InstanceTicket, error) {
	sceneId := cond.SceneId
	reqType := "共享"
	if !isSharedReq {
		reqType = "独占"
	}
	logger.Zap.Infof("【分配-场景】开始场景分配 sceneId=%q clientIP=%s 请求类型=%s 候选=%d 个", sceneId, cond.ClientIP, reqType, len(candidates))
	for _, pass := range []bool{true, false} {
		passName := "无白名单段"
		if pass {
			passName = "白名单命中段"
		}
		logger.Zap.Infof("【分配-场景】进入%s", passName)
		// 优先级1：已加载目标场景的匹配实例
		skipped := 0
		for _, instance := range candidates {
			if pass != whitelistHit(instance, cond.ClientIP) {
				continue
			}
			if instance.CurrentPak != sceneId || instance.StateCode != 1 {
				skipped++
				continue
			}
			ticketId, err := s.Reserve(instance.SID, cond.ClientIP, isSharedReq)
			if err != nil {
				logger.Zap.Infof("【分配-场景】[%s][优先级1] 预留失败 SID=%s 名称=%s err=%v（继续下一个）", passName, instance.SID, instance.Name, err)
				continue
			}
			instance.SceneId = sceneId
			InstanceService.UpdateInstance(instance)
			ticket.SetInstanceInfo(instance)
			ticket.Ticket = ticketId
			logger.Zap.Infof("【分配-场景】[%s][优先级1已加载目标场景] 选中 SID=%s 名称=%s，已签发ticket", passName, instance.SID, instance.Name)
			return ticket, nil
		}
		if skipped > 0 {
			logger.Zap.Infof("【分配-场景】[%s][优先级1] %d 个实例不满足（场景未加载或实例未运行）", passName, skipped)
		}
		// 优先级2：空闲实例（未开启或开启后未加载场景）
		skipped = 0
		for _, instance := range candidates {
			if pass != whitelistHit(instance, cond.ClientIP) {
				continue
			}
			if !instance.IsIdle() {
				skipped++
				continue
			}
			ticketId, err := s.Reserve(instance.SID, cond.ClientIP, isSharedReq)
			if err != nil {
				logger.Zap.Infof("【分配-场景】[%s][优先级2] 预留失败 SID=%s 名称=%s err=%v（继续下一个）", passName, instance.SID, instance.Name, err)
				continue
			}
			instance.SceneId = sceneId
			InstanceService.UpdateInstance(instance)
			s.cache.SetWithExpire(instance.SID, sceneId, ticketExpire)
			ticket.SetInstanceInfo(instance)
			ticket.Ticket = ticketId
			logger.Zap.Infof("【分配-场景】[%s][优先级2空闲实例] 选中 SID=%s 名称=%s，已签发ticket，即将加载场景", passName, instance.SID, instance.Name)
			return ticket, nil
		}
		if skipped > 0 {
			logger.Zap.Infof("【分配-场景】[%s][优先级2] %d 个实例不满足（非空闲）", passName, skipped)
		}
		// 优先级3：预备实例（连接数为0），命中后 LoadBundle 切换场景
		skipped = 0
		for _, instance := range candidates {
			if pass != whitelistHit(instance, cond.ClientIP) {
				continue
			}
			if instance.IsAccessing() {
				skipped++
				continue
			}
			ticketId, err := s.Reserve(instance.SID, cond.ClientIP, isSharedReq)
			if err != nil {
				logger.Zap.Infof("【分配-场景】[%s][优先级3] 预留失败 SID=%s 名称=%s err=%v（继续下一个）", passName, instance.SID, instance.Name, err)
				continue
			}
			instance.SceneId = sceneId
			InstanceService.UpdateInstance(instance)
			ticket.SetInstanceInfo(instance)
			ticket.Ticket = ticketId
			logger.Zap.Infof("【分配-场景】[%s][优先级3连接数为0] 选中 SID=%s 名称=%s，已签发ticket，切换场景", passName, instance.SID, instance.Name)
			s.LoadBundle(instance.SID, instance.SceneId)
			return ticket, nil
		}
		if skipped > 0 {
			logger.Zap.Infof("【分配-场景】[%s][优先级3] %d 个实例不满足（正在被访问）", passName, skipped)
		}
		// 优先级4：共享实例（开启共享且请求为共享类型），命中后 LoadBundle
		if !isSharedReq {
			logger.Zap.Infof("【分配-场景】[%s] 请求为独占，跳过优先级4（共享实例）", passName)
			continue
		}
		skipped = 0
		for _, instance := range candidates {
			if pass != whitelistHit(instance, cond.ClientIP) {
				continue
			}
			if !instance.EnableSharedInstance {
				skipped++
				continue
			}
			ticketId, err := s.Reserve(instance.SID, cond.ClientIP, isSharedReq)
			if err != nil {
				logger.Zap.Infof("【分配-场景】[%s][优先级4] 预留失败 SID=%s 名称=%s err=%v（继续下一个）", passName, instance.SID, instance.Name, err)
				continue
			}
			instance.SceneId = sceneId
			InstanceService.UpdateInstance(instance)
			ticket.SetInstanceInfo(instance)
			ticket.Ticket = ticketId
			logger.Zap.Infof("【分配-场景】[%s][优先级4共享实例] 选中 SID=%s 名称=%s，已签发ticket", passName, instance.SID, instance.Name)
			s.LoadBundle(instance.SID, instance.SceneId)
			return ticket, nil
		}
		if skipped > 0 {
			logger.Zap.Infof("【分配-场景】[%s][优先级4] %d 个实例不满足（未开启共享实例）", passName, skipped)
		}
	}
	logger.Zap.Warnf("【分配-场景】全部优先级落空 sceneId=%q clientIP=%s", sceneId, cond.ClientIP)
	return ticket, errors.New("未找到空闲实例，需要开启共享实例")
}

// whitelistHit 白名单命中判断：空白名单视为不命中（归第二段"无白名单段"）；
// 配置了白名单且命中其中一条规则（精确 IP 或 IPv4 通配网段 192.168.1.*）才命中第一段。
func whitelistHit(instance *model.ServerInstance, clientIP string) bool {
	if len(instance.Whitelist) == 0 {
		return false
	}
	return util.MatchIPRules(instance.Whitelist, clientIP)
}

// whitelistAllows 白名单准入判断（严格语义）：未配置白名单对所有 IP 开放；
// 配置了白名单则只有命中的 IP 可以分配到该实例，未命中的一律不参与分配，也不能通过 SID 直选绕过。
func whitelistAllows(instance *model.ServerInstance, clientIP string) bool {
	if len(instance.Whitelist) == 0 {
		return true
	}
	return util.MatchIPRules(instance.Whitelist, clientIP)
}

// instanceBrief 实例分配视图摘要，用于筛选流程日志
func instanceBrief(instance *model.ServerInstance, clientIP string) string {
	typeText := "共享"
	if instance.InstanceType == 1 {
		typeText = "独占"
	}
	return fmt.Sprintf("SID=%s 名称=%s cid=%d clientId=%d 类型=%s 白名单=%v 白名单匹配=%s 玩家数=%d 上限=%d 状态=%d 自动启停=%v 流已连=%v 当前场景=%q",
		instance.SID, instance.Name, instance.CID, instance.ClientID, typeText,
		instance.Whitelist, whitelistMatchBrief(instance.Whitelist, clientIP),
		instance.PlayerCount, instance.MaxPlayerCount, instance.StateCode, instance.AutoControl,
		instance.StreamerConnected, instance.CurrentPak)
}

// whitelistMatchBrief 白名单匹配摘要：未配置/命中/未命中
func whitelistMatchBrief(rules model.StringSlice, clientIP string) string {
	if len(rules) == 0 {
		return "未配置"
	}
	if util.MatchIPRules(rules, clientIP) {
		return "命中"
	}
	return "未命中"
}

// ruleMatchBrief 逐条规则匹配明细，用于诊断白名单不命中的原因
func ruleMatchBrief(rules model.StringSlice, clientIP string) string {
	if len(rules) == 0 {
		return "无规则"
	}
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		if util.MatchIPRule(rule, clientIP) {
			parts = append(parts, fmt.Sprintf("%s=命中", rule))
		} else {
			parts = append(parts, fmt.Sprintf("%s=未命中", rule))
		}
	}
	return strings.Join(parts, " ")
}

func (s *ticketService) LoadBundle(sid string, pak string) error {
	control := request.PakControl{
		SID:  sid,
		Type: "load",
		Pak:  "Paks/" + pak,
	}
	return InstanceService.PakControl(control)
}
