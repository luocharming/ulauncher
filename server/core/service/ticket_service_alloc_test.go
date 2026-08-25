package service

import (
	"os"
	"testing"

	"thingue-launcher/common/logger"
	"thingue-launcher/common/model"
	"thingue-launcher/common/request"
	"thingue-launcher/common/response"
	"thingue-launcher/server/global"

	"github.com/bluele/gcache"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 需求 5.1/5.2 分配策略回归测试。
// 每个用例使用独立的内存库（DSN 唯一），并重置 ticketService 的预留状态，
// 避免包级单例 TicketService 与进程级共享内存库在用例间互相污染。

// setupAlloc 建立隔离的运行时库 + 干净的预留状态，返回清理函数。
func setupAlloc(t *testing.T, dbName string) {
	t.Helper()
	logger.InitZapLogger("error", os.DevNull)

	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.ServerInstance{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	origServer := global.SERVER_DB
	global.SERVER_DB = db

	// 重置预留状态：Reserve 的容量判断依赖 sidReserved，跨用例残留会导致误判已占用
	TicketService.resMu.Lock()
	TicketService.reservations = make(map[string]*ticketReservation)
	TicketService.sidReserved = make(map[string]uint)
	TicketService.cache = gcache.New(1024).LRU().Build()
	TicketService.resMu.Unlock()

	t.Cleanup(func() {
		global.SERVER_DB = origServer
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
}

// mustCreate 直接写入运行时库，失败立即终止用例（避免"表不存在/主键冲突"被静默吞掉后误判分配结果）
func mustCreate(t *testing.T, instance *model.ServerInstance) {
	t.Helper()
	if err := global.SERVER_DB.Create(instance).Error; err != nil {
		t.Fatalf("写入实例 %s 失败: %v", instance.SID, err)
	}
}

// TestAllocExclusiveWhitelistFirst 需求 5.1：
// 独占请求先分配白名单命中的实例；白名单不命中时按顺序回落到无白名单的独占实例。
func TestAllocExclusiveWhitelistFirst(t *testing.T) {
	setupAlloc(t, "alloc_excl_first")
	// c_id 顺序：1=无白名单 2=白名单[10.1.2.3] 3=无白名单
	// 白名单实例故意排在中间，验证"白名单优先"不是靠顺序碰巧成立
	mustCreate(t, &model.ServerInstance{
		SID: "excl-nowl-1", CID: 1, ClientID: 1, Name: "excl-nowl-1",
		InstanceType: 1, StateCode: 1,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "excl-wl", CID: 2, ClientID: 1, Name: "excl-wl",
		InstanceType: 1, StateCode: 1, Whitelist: model.StringSlice{"10.1.2.3"},
	})
	mustCreate(t, &model.ServerInstance{
		SID: "excl-nowl-2", CID: 3, ClientID: 1, Name: "excl-nowl-2",
		InstanceType: 1, StateCode: 1,
	})

	// 白名单命中 IP：即使白名单实例 c_id 靠后，也必须优先分配
	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.3"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("白名单命中应分配成功: %v", err)
	}
	if ticket.SID != "excl-wl" {
		t.Fatalf("白名单命中应优先分配 excl-wl，实际 %s", ticket.SID)
	}

	// 白名单外 IP：跳过白名单实例，按顺序取第一个无白名单实例
	ticket2, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.9.9.9"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("白名单外 IP 应回落到无白名单实例: %v", err)
	}
	if ticket2.SID != "excl-nowl-1" {
		t.Fatalf("应按顺序回落到 excl-nowl-1，实际 %s", ticket2.SID)
	}
}

// TestAllocExclusiveWhitelistOccupiedFallback 需求 5.1：
// 白名单命中但该实例已被占用时，回落到无白名单的独占实例。
func TestAllocExclusiveWhitelistOccupiedFallback(t *testing.T) {
	setupAlloc(t, "alloc_excl_occupied")
	mustCreate(t, &model.ServerInstance{
		SID: "excl-wl", CID: 1, ClientID: 1, Name: "excl-wl",
		InstanceType: 1, StateCode: 1, PlayerCount: 1, // 已占用
		Whitelist: model.StringSlice{"10.1.2.3"},
	})
	mustCreate(t, &model.ServerInstance{
		SID: "excl-nowl", CID: 2, ClientID: 1, Name: "excl-nowl",
		InstanceType: 1, StateCode: 1,
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.3"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("白名单实例被占用时应回落: %v", err)
	}
	if ticket.SID != "excl-nowl" {
		t.Fatalf("应回落到 excl-nowl，实际 %s", ticket.SID)
	}
}

// TestAllocExclusiveAllOccupied 需求 5.1 第 2 条：所有独占实例都被占用则不分配。
func TestAllocExclusiveAllOccupied(t *testing.T) {
	setupAlloc(t, "alloc_excl_all_busy")
	mustCreate(t, &model.ServerInstance{
		SID: "excl-1", CID: 1, ClientID: 1, Name: "excl-1",
		InstanceType: 1, StateCode: 1, PlayerCount: 1,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "excl-2", CID: 2, ClientID: 1, Name: "excl-2",
		InstanceType: 1, StateCode: 1, PlayerCount: 1,
	})

	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.3"},
		response.InstanceTicket{}); err == nil {
		t.Fatal("所有独占实例已占用时应返回错误")
	}
}

// TestAllocExclusiveConcurrentReserve 独占语义：同一实例不能被两个请求同时预留。
func TestAllocExclusiveConcurrentReserve(t *testing.T) {
	setupAlloc(t, "alloc_excl_race")
	mustCreate(t, &model.ServerInstance{
		SID: "excl-only", CID: 1, ClientID: 1, Name: "excl-only",
		InstanceType: 1, StateCode: 1,
	})

	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.3"},
		response.InstanceTicket{}); err != nil {
		t.Fatalf("第一个请求应成功: %v", err)
	}
	// 第一个请求已预留（未配对消费），第二个请求必须失败
	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.4"},
		response.InstanceTicket{}); err == nil {
		t.Fatal("独占实例已被预留，第二个请求应失败")
	}
}

// TestAllocSharedWhitelistFirst 需求 5.2：
// 共享请求先分配白名单命中且未满的实例；白名单不命中时按顺序取第一个未满的无白名单实例。
func TestAllocSharedWhitelistFirst(t *testing.T) {
	setupAlloc(t, "alloc_shared_first")
	// 白名单实例排在中间，且已有 1 个连接（未满）——需求只要求"未满"即可分配
	mustCreate(t, &model.ServerInstance{
		SID: "shared-nowl-1", CID: 1, ClientID: 1, Name: "shared-nowl-1",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: 5,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "shared-wl", CID: 2, ClientID: 1, Name: "shared-wl",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: 5, PlayerCount: 1,
		Whitelist: model.StringSlice{"10.1.2.3"},
	})
	mustCreate(t, &model.ServerInstance{
		SID: "shared-nowl-2", CID: 3, ClientID: 1, Name: "shared-nowl-2",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: 5,
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.1.2.3"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("共享白名单命中应分配成功: %v", err)
	}
	if ticket.SID != "shared-wl" {
		t.Fatalf("白名单命中应优先分配 shared-wl，实际 %s", ticket.SID)
	}

	ticket2, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.9.9.9"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("白名单外 IP 应回落: %v", err)
	}
	if ticket2.SID != "shared-nowl-1" {
		t.Fatalf("应按顺序回落到 shared-nowl-1，实际 %s", ticket2.SID)
	}
}

// TestAllocSharedSkipFull 需求 5.2：最靠前的共享实例已满时，按顺序查找下一个。
func TestAllocSharedSkipFull(t *testing.T) {
	setupAlloc(t, "alloc_shared_skip_full")
	mustCreate(t, &model.ServerInstance{
		SID: "shared-full", CID: 1, ClientID: 1, Name: "shared-full",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: 2, PlayerCount: 2, // 已满
	})
	mustCreate(t, &model.ServerInstance{
		SID: "shared-free", CID: 2, ClientID: 1, Name: "shared-free",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: 2,
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.1.2.3"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("应跳过已满实例分配下一个: %v", err)
	}
	if ticket.SID != "shared-free" {
		t.Fatalf("应分配 shared-free，实际 %s", ticket.SID)
	}
}

// TestAllocSharedUnlimited MaxPlayerCount=-1（默认）永不判满。
func TestAllocSharedUnlimited(t *testing.T) {
	setupAlloc(t, "alloc_shared_unlimited")
	mustCreate(t, &model.ServerInstance{
		SID: "shared-unlim", CID: 1, ClientID: 1, Name: "shared-unlim",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1, PlayerCount: 999,
	})

	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.1.2.3"},
		response.InstanceTicket{}); err != nil {
		t.Fatalf("不限连接数的共享实例应永不判满: %v", err)
	}
}

// TestAllocWhitelistNotMatchedExcluded 白名单配置了但 IP 不命中的实例不参与任何一段分配：
// 只有白名单实例且 IP 不命中时，必须报错而不是把该实例分配出去。
func TestAllocWhitelistNotMatchedExcluded(t *testing.T) {
	setupAlloc(t, "alloc_wl_excluded")
	mustCreate(t, &model.ServerInstance{
		SID: "shared-wl-only", CID: 1, ClientID: 1, Name: "shared-wl-only",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
		Whitelist: model.StringSlice{"10.1.2.3"},
	})

	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.9.9.9"},
		response.InstanceTicket{}); err == nil {
		t.Fatal("IP 不在白名单内不应分配到该实例")
	}
}

// TestAllocWildcardWhitelist 通配网段白名单命中优先。
func TestAllocWildcardWhitelist(t *testing.T) {
	setupAlloc(t, "alloc_wildcard")
	mustCreate(t, &model.ServerInstance{
		SID: "shared-nowl", CID: 1, ClientID: 1, Name: "shared-nowl",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "shared-wildcard", CID: 2, ClientID: 1, Name: "shared-wildcard",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
		Whitelist: model.StringSlice{"192.168.1.*"},
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "192.168.1.77"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("通配网段命中应分配成功: %v", err)
	}
	if ticket.SID != "shared-wildcard" {
		t.Fatalf("通配网段命中应优先分配 shared-wildcard，实际 %s", ticket.SID)
	}
}

// TestAllocTypePoolNoCross 类型池不交叉：独占请求不会落到共享实例，反之亦然。
func TestAllocTypePoolNoCross(t *testing.T) {
	setupAlloc(t, "alloc_type_pool")
	mustCreate(t, &model.ServerInstance{
		SID: "shared-only", CID: 1, ClientID: 1, Name: "shared-only",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
	})

	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.3"},
		response.InstanceTicket{}); err == nil {
		t.Fatal("没有独占实例时独占请求应报错，而不是回落到共享实例")
	}
	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.1.2.3"},
		response.InstanceTicket{}); err != nil {
		t.Fatalf("共享请求应正常分配: %v", err)
	}
}

// TestAllocOldClientDefaultsShared 旧播放页不传 shared（nil）时按共享请求处理，且仍受白名单约束。
func TestAllocOldClientDefaultsShared(t *testing.T) {
	setupAlloc(t, "alloc_old_client")
	mustCreate(t, &model.ServerInstance{
		SID: "shared-nowl", CID: 1, ClientID: 1, Name: "shared-nowl",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "shared-wl", CID: 2, ClientID: 1, Name: "shared-wl",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
		Whitelist: model.StringSlice{"10.1.2.3"},
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: nil, ClientIP: "10.1.2.3"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("旧请求应按共享处理: %v", err)
	}
	if ticket.SID != "shared-wl" {
		t.Fatalf("旧请求也应先走白名单段，期望 shared-wl，实际 %s", ticket.SID)
	}
}

// TestAllocSidDirectRespectsWhitelist SID 直选不能绕过白名单准入。
func TestAllocSidDirectRespectsWhitelist(t *testing.T) {
	setupAlloc(t, "alloc_sid_direct")
	mustCreate(t, &model.ServerInstance{
		SID: "target", CID: 1, ClientID: 1, Name: "target",
		InstanceType: 1, StateCode: 1, Whitelist: model.StringSlice{"10.1.2.3"},
	})

	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.9.9.9", SID: "target"},
		response.InstanceTicket{}); err == nil {
		t.Fatal("SID 直选不应绕过白名单")
	}
	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.3", SID: "target"},
		response.InstanceTicket{}); err != nil {
		t.Fatalf("SID 直选白名单命中应成功: %v", err)
	}
}

// TestAllocSceneWhitelistFirst 携带 sceneId（thinguelib 的常规路径）时，
// 白名单命中段必须整体优先于无白名单段——即使无白名单实例在场景优先级上更"合适"。
func TestAllocSceneWhitelistFirst(t *testing.T) {
	setupAlloc(t, "alloc_scene_wl")
	// B 已加载目标场景（场景优先级最高），但没有白名单；
	// A 命中白名单但需要切换场景。按需求"先从配置了白名单的实例中筛选"，应分配 A。
	mustCreate(t, &model.ServerInstance{
		SID: "scene-nowl-loaded", CID: 1, ClientID: 1, Name: "scene-nowl-loaded",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1, CurrentPak: "scene-1",
	})
	mustCreate(t, &model.ServerInstance{
		SID: "scene-wl-idle", CID: 2, ClientID: 1, Name: "scene-wl-idle",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
		Whitelist: model.StringSlice{"10.1.2.3"},
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.1.2.3", SceneId: "scene-1"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("携带 sceneId 时白名单命中应分配成功: %v", err)
	}
	if ticket.SID != "scene-wl-idle" {
		t.Fatalf("携带 sceneId 时白名单段应优先，期望 scene-wl-idle，实际 %s", ticket.SID)
	}
}

// TestAllocSceneWhitelistNotMatchedFallback 携带 sceneId 且 IP 不命中白名单时，
// 回落到无白名单实例（白名单实例不参与）。
func TestAllocSceneWhitelistNotMatchedFallback(t *testing.T) {
	setupAlloc(t, "alloc_scene_fallback")
	mustCreate(t, &model.ServerInstance{
		SID: "scene-wl", CID: 1, ClientID: 1, Name: "scene-wl",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1, CurrentPak: "scene-1",
		Whitelist: model.StringSlice{"10.1.2.3"},
	})
	mustCreate(t, &model.ServerInstance{
		SID: "scene-nowl", CID: 2, ClientID: 1, Name: "scene-nowl",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: -1,
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.9.9.9", SceneId: "scene-1"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("白名单外 IP 应回落到无白名单实例: %v", err)
	}
	if ticket.SID != "scene-nowl" {
		t.Fatalf("期望回落到 scene-nowl，实际 %s", ticket.SID)
	}
}

func boolPtr(b bool) *bool {
	return &b
}
