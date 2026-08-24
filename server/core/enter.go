package core

import (
	"thingue-launcher/server/core/service"
)

var (
	ClientService   = service.ClientService
	InstanceService = service.InstanceService
	// TicketService 含 sync.Mutex，必须取指针别名，避免复制出第二把锁
	// （按值复制会导致 core.TicketService 与 service.TicketService 各自加锁，同一批 map 失去互斥保护）
	TicketService   = &service.TicketService
	SyncService     = service.SyncService
	SdpService      = service.SdpService
)
