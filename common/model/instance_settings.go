package model

import "time"

// InstanceSettings 实例分配设置（类型/白名单/连接数上限），持久化在 STORAGE_DB（storage.db），
// 以 SID 为键关联 ClientInstance.SID；客户端每次 clientRegister 时合并到 SERVER_DB 的 ServerInstance 上。
// 管理端是唯一编辑入口（updateInstanceSettings），last-write-wins。
type InstanceSettings struct {
	ID             uint        `json:"id" gorm:"primarykey"`
	SID            string      `json:"sid" gorm:"uniqueIndex;size:64"`  // 关联 ClientInstance.SID
	InstanceType   int8        `json:"instanceType" gorm:"default:0"`   // 0=共享 1=独占
	Whitelist      StringSlice `json:"whitelist"`                       // 白名单 IP；空=不过滤
	MaxPlayerCount int         `json:"maxPlayerCount" gorm:"default:-1"` // 共享连接数上限：-1=不限；独占忽略
	LastSeenAt     time.Time   `json:"lastSeenAt"`                      // 最近一次 register 合并时间，孤儿行清理依据
	UpdatedAt      time.Time   `json:"updatedAt"`
	CreatedAt      time.Time   `json:"createdAt"`
}
