package util

import "testing"

func TestNormalizeIPRule(t *testing.T) {
	cases := []struct {
		rule string
		want string
	}{
		{"192.168.1.10", "192.168.1.10"},
		{" 192.168.1.10 ", "192.168.1.10"},
		{"192.168.1.*", "192.168.1.*"},
		{"192.168.001.*", "192.168.1.*"}, // 前导零归一化
		{"10.*.*.*", "10.*.*.*"},
		{"*.*.*.*", "*.*.*.*"},
		{"fe80::1", "fe80::1"},
		{"", ""},
		{"192.168.1", ""},
		{"192.168.1.256", ""},
		{"192.168.1.*.5", ""},
		{"192.168.*", ""},   // 通配规则必须是完整 4 段
		{"192.168.1.a", ""}, // 非数字段
		{"192.168.1.**", ""},
		{"not-an-ip", ""},
	}
	for _, c := range cases {
		if got := NormalizeIPRule(c.rule); got != c.want {
			t.Errorf("NormalizeIPRule(%q) = %q, want %q", c.rule, got, c.want)
		}
	}
}

func TestMatchIPRule(t *testing.T) {
	cases := []struct {
		rule string
		ip   string
		want bool
	}{
		{"192.168.1.10", "192.168.1.10", true},
		{"192.168.1.10", "192.168.1.11", false},
		{"192.168.1.*", "192.168.1.10", true},
		{"192.168.1.*", "192.168.1.255", true},
		{"192.168.1.*", "192.168.2.10", false},
		{"10.*.*.*", "10.20.30.40", true},
		{"10.*.*.*", "11.20.30.40", false},
		{"192.*.1.*", "192.168.1.7", true},
		{"192.*.1.*", "192.168.2.7", false},
		{"192.168.1.*", "fe80::1", false}, // 通配网段不作用于 IPv6
		{"fe80::1", "fe80::0001", true},   // IPv6 规范化后比较
		{"192.168.1.*", "", false},
		{"", "192.168.1.10", false},
		{"192.168.1.*", "not-an-ip", false},
	}
	for _, c := range cases {
		if got := MatchIPRule(c.rule, c.ip); got != c.want {
			t.Errorf("MatchIPRule(%q, %q) = %v, want %v", c.rule, c.ip, got, c.want)
		}
	}
}

func TestMatchIPRules(t *testing.T) {
	rules := []string{"10.0.0.5", "192.168.1.*"}
	if !MatchIPRules(rules, "192.168.1.99") {
		t.Error("期望命中通配网段 192.168.1.*")
	}
	if !MatchIPRules(rules, "10.0.0.5") {
		t.Error("期望命中精确 IP 10.0.0.5")
	}
	if MatchIPRules(rules, "10.0.0.6") {
		t.Error("10.0.0.6 不应命中任何规则")
	}
	if MatchIPRules(nil, "10.0.0.5") {
		t.Error("空规则列表应返回 false")
	}
}
