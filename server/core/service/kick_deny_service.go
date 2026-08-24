package service

import (
	"errors"
	"sync"
	"thingue-launcher/common/provider"
	"time"
)

// ErrKickedDenied 被踢 IP 拒绝名单拦截错误，handler 侧识别后转 403
var ErrKickedDenied = errors.New("已被管理员断开连接，请稍后重试")

// denyService 被踢 IP 拒绝名单（问题1 规避的第一层/权威层）：
// 踢人后写入 IP，TTL（localServer.kickDenySeconds，默认 60s，<=0 视为不拒绝）内
// ticketSelect/ws升级/配对三次检查拦截，防止客户端自动重连使断开操作失效。
type denyService struct {
	mu     sync.Mutex
	denied map[string]time.Time // ip → expireAt
}

var DenyService = &denyService{denied: make(map[string]time.Time)}

func (d *denyService) Add(ip string) {
	if ip == "" {
		return
	}
	seconds := provider.AppConfig.LocalServer.KickDenySeconds
	if seconds <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.denied[ip] = time.Now().Add(time.Duration(seconds) * time.Second)
}

// IsDenied 惰性清理过期条目后判断 IP 是否在拒绝名单内
func (d *denyService) IsDenied(ip string) bool {
	if ip == "" || provider.AppConfig.LocalServer.KickDenySeconds <= 0 {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for k, expireAt := range d.denied {
		if now.After(expireAt) {
			delete(d.denied, k)
		}
	}
	_, ok := d.denied[ip]
	return ok
}
