package service

import (
	"testing"

	"thingue-launcher/common/model"
)

func TestWhitelistHit(t *testing.T) {
	cases := []struct {
		name      string
		whitelist model.StringSlice
		clientIP  string
		want      bool
	}{
		{"空白名单不命中", nil, "10.0.0.1", false},
		{"精确命中", model.StringSlice{"10.0.0.1"}, "10.0.0.1", true},
		{"精确未命中", model.StringSlice{"10.0.0.2"}, "10.0.0.1", false},
		{"通配网段命中", model.StringSlice{"192.168.1.*"}, "192.168.1.99", true},
		{"通配网段未命中", model.StringSlice{"192.168.1.*"}, "192.168.2.1", false},
		{"多规则其一命中", model.StringSlice{"10.0.0.2", "192.168.1.*"}, "192.168.1.7", true},
		{"非法客户端IP不命中", model.StringSlice{"192.168.1.*"}, "not-an-ip", false},
	}
	for _, c := range cases {
		instance := &model.ServerInstance{Whitelist: c.whitelist}
		if got := whitelistHit(instance, c.clientIP); got != c.want {
			t.Errorf("%s: whitelistHit(%v, %q) = %v, want %v", c.name, c.whitelist, c.clientIP, got, c.want)
		}
	}
}

func TestWhitelistAllows(t *testing.T) {
	cases := []struct {
		name      string
		whitelist model.StringSlice
		clientIP  string
		want      bool
	}{
		{"未配置白名单对所有IP开放", nil, "10.0.0.1", true},
		{"空数组同样开放", model.StringSlice{}, "10.0.0.1", true},
		{"配置后未命中拒绝", model.StringSlice{"10.0.0.2"}, "10.0.0.1", false},
		{"配置后命中放行", model.StringSlice{"10.0.0.1"}, "10.0.0.1", true},
		{"配置通配网段命中放行", model.StringSlice{"192.168.1.*"}, "192.168.1.123", true},
		{"配置通配网段未命中拒绝", model.StringSlice{"192.168.1.*"}, "192.168.2.123", false},
		{"非法客户端IP拒绝", model.StringSlice{"10.0.0.1"}, "not-an-ip", false},
	}
	for _, c := range cases {
		instance := &model.ServerInstance{Whitelist: c.whitelist}
		if got := whitelistAllows(instance, c.clientIP); got != c.want {
			t.Errorf("%s: whitelistAllows(%v, %q) = %v, want %v", c.name, c.whitelist, c.clientIP, got, c.want)
		}
	}
}
