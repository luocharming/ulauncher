package util

import (
	"net"
	"strconv"
	"strings"
)

// NormalizeIP 用 net.ParseIP 规范化 IP 字符串，比较与存储统一格式（兼容 IPv4/IPv6）；
// 解析失败返回空字符串
func NormalizeIP(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

// NormalizeIPRule 规范化一条白名单规则：支持精确 IP（IPv4/IPv6）与 IPv4 通配网段（如 192.168.1.*）。
// 通配符 * 代表该段任意取值，可出现在任意段位（192.168.1.* / 10.*.*.*）；非法规则返回空字符串。
func NormalizeIPRule(rule string) string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return ""
	}
	if !strings.Contains(rule, "*") {
		return NormalizeIP(rule)
	}
	segments := strings.Split(rule, ".")
	if len(segments) != 4 {
		return ""
	}
	normalized := make([]string, 4)
	for i, segment := range segments {
		if segment == "*" {
			normalized[i] = "*"
			continue
		}
		num, ok := parseIPv4Segment(segment)
		if !ok {
			return ""
		}
		normalized[i] = strconv.Itoa(num) // 去掉前导零，与匹配时的比较格式统一
	}
	return strings.Join(normalized, ".")
}

// MatchIPRule 判断 IP 是否命中单条白名单规则（精确 IP 或 IPv4 通配网段）；
// 通配网段只作用于 IPv4，IPv6 只能用精确匹配。
func MatchIPRule(rule, ip string) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" || ip == "" {
		return false
	}
	if !strings.Contains(rule, "*") {
		normalizedIP := NormalizeIP(ip)
		return normalizedIP != "" && normalizedIP == NormalizeIP(rule)
	}
	segments := strings.Split(rule, ".")
	if len(segments) != 4 {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	ip4 := parsed.To4()
	if ip4 == nil {
		return false
	}
	for i, segment := range segments {
		if segment == "*" {
			continue
		}
		num, ok := parseIPv4Segment(segment)
		if !ok || num != int(ip4[i]) {
			return false
		}
	}
	return true
}

// MatchIPRules 任一规则命中即命中；空规则列表返回 false（"空白名单不过滤"的语义由调用方判断）
func MatchIPRules(rules []string, ip string) bool {
	for _, rule := range rules {
		if MatchIPRule(rule, ip) {
			return true
		}
	}
	return false
}

// parseIPv4Segment 解析 IPv4 单段（仅数字、0-255，最多 3 位）
func parseIPv4Segment(segment string) (int, bool) {
	if segment == "" || len(segment) > 3 {
		return 0, false
	}
	for _, c := range segment {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	num, err := strconv.Atoi(segment)
	if err != nil || num < 0 || num > 255 {
		return 0, false
	}
	return num, true
}
