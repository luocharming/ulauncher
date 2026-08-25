package service

import (
	"testing"

	"thingue-launcher/common/model"
)

// TestRenewKeepsOwnReservation 冷启动连锁拉起实例的回归测试。
//
// 实例自动启动完成后必须"续期玩家自己的预留"，而不是重新 Reserve：
// 自动启动通常 2~4s 就完成，此时玩家自己的预留仍在 10s TTL 内，
// 重新预留会把这份预留算成占用者，独占实例必然「实例已被独占占用」，
// 于是玩家被断开 → SDK 重连 → 重新 ticketSelect → 分到下一台实例再拉起。
func TestRenewKeepsOwnReservation(t *testing.T) {
	setupAlloc(t, "renew_own_reservation")
	mustCreate(t, &model.ServerInstance{
		SID: "excl", CID: 1, ClientID: 1, Name: "excl",
		InstanceType: 1, StateCode: 1, AutoControl: true,
	})

	// 分配阶段签发 ticket，此时 sidReserved["excl"] = 1
	ticket, err := TicketService.Reserve("excl", "10.0.0.1", false)
	if err != nil {
		t.Fatalf("首次预留应成功: %v", err)
	}

	// 用例前提：自动启动完成后若重新 Reserve，会被自己的预留判成已占用
	if _, err := TicketService.Reserve("excl", "10.0.0.1", false); err == nil {
		t.Fatal("重新预留本应被自己的预留挡住，却成功了——用例前提已失效")
	}

	// 修复后的行为：续期原预留，ticket 保持有效，配对时能正常消费
	if !TicketService.Renew(ticket, "excl") {
		t.Fatal("未过期的预留应能续期")
	}
	if sid, err := TicketService.GetSidByTicket(ticket); err != nil || sid != "excl" {
		t.Fatalf("续期后 ticket 应仍可解析: sid=%q err=%v", sid, err)
	}
	if err := TicketService.Consume(ticket, "excl"); err != nil {
		t.Fatalf("续期后的 ticket 应能正常配对消费: %v", err)
	}
}

// TestRenewRejectsInvalidReservation 续期只对"仍属于该实例且未消费"的预留生效，
// 不能成为绕过容量判据凭空造预留的口子。
func TestRenewRejectsInvalidReservation(t *testing.T) {
	setupAlloc(t, "renew_invalid")
	mustCreate(t, &model.ServerInstance{
		SID: "excl", CID: 1, ClientID: 1, Name: "excl",
		InstanceType: 1, StateCode: 1, AutoControl: true,
	})
	mustCreate(t, &model.ServerInstance{
		SID: "other", CID: 2, ClientID: 1, Name: "other",
		InstanceType: 1, StateCode: 1, AutoControl: true,
	})

	if TicketService.Renew("不存在的ticket", "excl") {
		t.Fatal("未知 ticket 不应续期成功")
	}

	ticket, err := TicketService.Reserve("excl", "10.0.0.1", false)
	if err != nil {
		t.Fatalf("预留应成功: %v", err)
	}
	if TicketService.Renew(ticket, "other") {
		t.Fatal("ticket 与 sid 不匹配时不应续期成功")
	}
	if err := TicketService.Consume(ticket, "excl"); err != nil {
		t.Fatalf("消费应成功: %v", err)
	}
	if TicketService.Renew(ticket, "excl") {
		t.Fatal("已消费的 ticket 不应续期成功")
	}
}

// TestRenewedReservationStillBlocksOthers 续期期间独占实例仍对其他玩家保持占用，
// 即冷启动这几秒里实例不会被别人抢走。
func TestRenewedReservationStillBlocksOthers(t *testing.T) {
	setupAlloc(t, "renew_blocks_others")
	mustCreate(t, &model.ServerInstance{
		SID: "excl", CID: 1, ClientID: 1, Name: "excl",
		InstanceType: 1, StateCode: 1, AutoControl: true,
	})

	ticket, err := TicketService.Reserve("excl", "10.0.0.1", false)
	if err != nil {
		t.Fatalf("首次预留应成功: %v", err)
	}
	if !TicketService.Renew(ticket, "excl") {
		t.Fatal("未过期的预留应能续期")
	}
	if _, err := TicketService.Reserve("excl", "10.0.0.2", false); err == nil {
		t.Fatal("续期期间其他玩家不应能预留同一台独占实例")
	}
}
