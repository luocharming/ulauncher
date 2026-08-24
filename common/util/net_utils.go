package util

import "net"

// NormalizeIP 用 net.ParseIP 规范化 IP 字符串，比较与存储统一格式（兼容 IPv4/IPv6）；
// 解析失败返回空字符串
func NormalizeIP(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	return parsed.String()
}
