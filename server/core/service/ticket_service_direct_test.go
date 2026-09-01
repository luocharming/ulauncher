package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"thingue-launcher/common/model"
	"thingue-launcher/common/request"
	"thingue-launcher/common/response"
	"thingue-launcher/server/core/provider"

	"github.com/gorilla/websocket"
)

// 需求第 3 条（独占实例恒 1 连接）与遗留 player_count 预过滤清理的回归测试。

// TestAllocExclusiveSecondFreeInstance 问题 2：第一个独占被占用后，
// 独占请求应按顺序分配到第二个空闲独占实例，而不是报错。
func TestAllocExclusiveSecondFreeInstance(t *testing.T) {
	setupAlloc(t, "alloc_excl_second_free")
	mustCreate(t, &model.ServerInstance{
		SID: "excl-1", CID: 1, ClientID: 1, Name: "excl-1",
		InstanceType: 1, StateCode: 1, PlayerCount: 1, // 已占用
	})
	mustCreate(t, &model.ServerInstance{
		SID: "excl-2", CID: 2, ClientID: 1, Name: "excl-2",
		InstanceType: 1, StateCode: 1, // 空闲
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.3"},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("第一个独占被占用时应分配第二个空闲独占: %v", err)
	}
	if ticket.SID != "excl-2" {
		t.Fatalf("应分配 excl-2，实际 %s", ticket.SID)
	}
	// 第二台已被预留（未消费），第三个独占请求应失败
	if _, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(false), ClientIP: "10.1.2.4"},
		response.InstanceTicket{}); err == nil {
		t.Fatal("两台独占均被预留后应报错")
	}
}

// TestAllocPlayerCountParamNotPrefiltering 请求携带 playerCount 参数不再作为 DB 预过滤条件：
// playerCount=0 时旧实现会按 player_count < 0 清空候选返回"连接数已满"，
// 现在容量完全由 Reserve 的原子判据决定。
func TestAllocPlayerCountParamNotPrefiltering(t *testing.T) {
	setupAlloc(t, "alloc_player_count_param")
	mustCreate(t, &model.ServerInstance{
		SID: "shared", CID: 1, ClientID: 1, Name: "shared",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: 5, PlayerCount: 1,
	})

	ticket, err := TicketService.selectInstance(
		request.SelectorCond{Shared: boolPtr(true), ClientIP: "10.1.2.3", PlayerCount: intPtr(0)},
		response.InstanceTicket{})
	if err != nil {
		t.Fatalf("playerCount=0 不应清空候选，应按 Reserve 判据分配: %v", err)
	}
	if ticket.SID != "shared" {
		t.Fatalf("应分配 shared，实际 %s", ticket.SID)
	}
}

// TestGetTicketByIdExclusiveCapacity getTicketById 指名直选对独占实例执行独占容量判据，
// 共享实例保持必成功。
func TestGetTicketByIdExclusiveCapacity(t *testing.T) {
	setupAlloc(t, "alloc_get_ticket_by_id")
	mustCreate(t, &model.ServerInstance{
		SID: "excl-occupied", CID: 1, ClientID: 1, Name: "excl-occupied",
		InstanceType: 1, StateCode: 1, PlayerCount: 1,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "excl-free", CID: 2, ClientID: 1, Name: "excl-free",
		InstanceType: 1, StateCode: 1,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "shared-full", CID: 3, ClientID: 1, Name: "shared-full",
		InstanceType: 0, StateCode: 1, MaxPlayerCount: 1, PlayerCount: 1,
	})

	// 已占用的独占实例拒绝
	if _, err := TicketService.GetTicketById("excl-occupied", "10.1.2.3"); err == nil {
		t.Fatal("已占用的独占实例 getTicketById 应被拒绝")
	}
	// 空闲独占实例出票成功
	ticketId, err := TicketService.GetTicketById("excl-free", "10.1.2.3")
	if err != nil || ticketId == "" {
		t.Fatalf("空闲独占实例 getTicketById 应出票: err=%v ticket=%q", err, ticketId)
	}
	// 未消费的预留计入独占判据：第二个 IP 再取同一实例应失败
	if _, err := TicketService.GetTicketById("excl-free", "10.1.2.4"); err == nil {
		t.Fatal("已有未消费预留时 getTicketById 应被拒绝")
	}
	// 共享实例按策略已满仍必成功
	if ticketId, err := TicketService.GetTicketById("shared-full", "10.1.2.3"); err != nil || ticketId == "" {
		t.Fatalf("共享实例 getTicketById 应必成功: err=%v ticket=%q", err, ticketId)
	}
}

// TestOnPlayerPairedExclusiveRejectsDirect 配对兜底对指名直选签发的票同样生效：
// 独占实例已有玩家时，Direct 票配对也必须被拒绝，独占实例恒为 1 个连接。
// 覆盖 Consume 与 UpdatePlayers 落库之间的窗口：预留已消费但玩家数尚未落库，
// 后续 Reserve 可能误判为空闲，这里以 streamer 实时玩家列表为最终判据。
func TestOnPlayerPairedExclusiveRejectsDirect(t *testing.T) {
	setupAlloc(t, "alloc_paired_direct")
	// PlayerCount=0 模拟"预留已消费但玩家数未落库"的窗口
	mustCreate(t, &model.ServerInstance{
		SID: "excl", CID: 1, ClientID: 1, Name: "excl",
		InstanceType: 1, StateCode: 1,
	})

	serverConn, existingConn, intruderConn := newPairedTestConns(t)
	streamer := provider.SdpConnProvider.NewStreamer("excl", serverConn, false, false)
	t.Cleanup(func() {
		streamer.Close()
		existingConn.Close()
		intruderConn.Close()
	})

	// 独占实例上已有一个玩家
	existing := provider.SdpConnProvider.NewPlayer(existingConn)
	existing.StreamerConnector = streamer
	streamer.AddPlayer(existing)
	t.Cleanup(func() { provider.SdpConnProvider.RemovePlayer(existing.PlayerId) })

	// 新玩家持指名直选签发的 ticket（direct=true）：兜底对 Direct 票同样拒绝
	player := provider.SdpConnProvider.NewPlayer(intruderConn)
	player.StreamerConnector = streamer
	player.IP = "10.1.2.3"
	player.Direct = true
	player.Ticket = TicketService.ReserveDirect("excl", player.IP)
	t.Cleanup(func() { provider.SdpConnProvider.RemovePlayer(player.PlayerId) })

	if err := SdpService.OnPlayerPaired(player); err == nil {
		t.Fatal("独占实例已有玩家时 Direct 票配对必须被拒绝")
	}
	if player.Paired {
		t.Fatal("被拒绝的玩家不应标记为已配对")
	}
}

// TestOnPlayerPairedExclusiveFreeSucceeds 独占实例空闲时正常配对。
func TestOnPlayerPairedExclusiveFreeSucceeds(t *testing.T) {
	setupAlloc(t, "alloc_paired_free")
	mustCreate(t, &model.ServerInstance{
		SID: "excl", CID: 1, ClientID: 1, Name: "excl",
		InstanceType: 1, StateCode: 1,
	})

	serverConn, playerConn, _ := newPairedTestConns(t)
	streamer := provider.SdpConnProvider.NewStreamer("excl", serverConn, false, false)
	t.Cleanup(func() {
		streamer.Close()
		playerConn.Close()
	})

	player := provider.SdpConnProvider.NewPlayer(playerConn)
	player.StreamerConnector = streamer
	player.IP = "10.1.2.3"
	ticketId, err := TicketService.Reserve("excl", player.IP, false)
	if err != nil {
		t.Fatalf("独占实例空闲时 Reserve 应成功: %v", err)
	}
	player.Ticket = ticketId
	t.Cleanup(func() { provider.SdpConnProvider.RemovePlayer(player.PlayerId) })

	if err := SdpService.OnPlayerPaired(player); err != nil {
		t.Fatalf("独占实例空闲时配对应成功: %v", err)
	}
	if !player.Paired {
		t.Fatal("配对成功后玩家应标记为已配对")
	}
}

// newPairedTestConns 建立三组真实 websocket 连接（streamer/已有玩家/新玩家），
// server 端起读循环保持连接存活。SendCloseMsg/SendPlayersCount 直接写 conn，
// 零值 *websocket.Conn 会 panic，必须用真实连接。
func newPairedTestConns(t *testing.T) (serverConn, connA, connB *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	upgradedCh := make(chan *websocket.Conn, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		upgradedCh <- conn
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	dial := func() *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("建立测试 websocket 连接失败: %v", err)
		}
		return conn
	}
	connA = dial() // 已有玩家
	connB = dial() // 新玩家
	// server 端按 dial 顺序完成两次 upgrade，第一个作为 streamer 的 conn
	serverConn = <-upgradedCh
	_ = <-upgradedCh
	return serverConn, connA, connB
}

func intPtr(v int) *int {
	return &v
}
