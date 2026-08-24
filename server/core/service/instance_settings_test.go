package service

import (
	"os"
	"testing"

	"thingue-launcher/common/logger"
	"thingue-launcher/common/model"
	"thingue-launcher/common/request"
	"thingue-launcher/server/global"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestUpdateInstanceSettingsPersistTwice 回归验证：同一实例连续保存两次必须覆盖成功。
// 历史 bug：GORM 的 OnConflict{UpdateAll} 默认按主键 id 生成 ON CONFLICT，新行 id 恒为 0，
// 第二次保存撞 s_id 唯一索引静默失败——SERVER_DB 内存库刷新/客户端重连后设置被旧值覆盖回滚。
func TestUpdateInstanceSettingsPersistTwice(t *testing.T) {
	logger.InitZapLogger("error", os.DevNull)

	origServer, origStorage := global.SERVER_DB, global.STORAGE_DB
	defer func() {
		global.SERVER_DB, global.STORAGE_DB = origServer, origStorage
	}()

	global.SERVER_DB = mustOpenMemory(t)
	global.STORAGE_DB = mustOpenTemp(t)
	if err := global.SERVER_DB.AutoMigrate(&model.ServerInstance{}); err != nil {
		t.Fatal(err)
	}
	if err := global.STORAGE_DB.AutoMigrate(&model.InstanceSettings{}); err != nil {
		t.Fatal(err)
	}

	global.SERVER_DB.Create(&model.ServerInstance{
		SID: "sid-1", CID: 1, ClientID: 1, Name: "inst-1",
	})

	save := func(instanceType int8, whitelist []string, maxPlayerCount int) (*model.ServerInstance, error) {
		return InstanceService.UpdateInstanceSettings(request.UpdateInstanceSettingsReq{
			SID:            "sid-1",
			InstanceType:   instanceType,
			Whitelist:      whitelist,
			MaxPlayerCount: maxPlayerCount,
		})
	}

	if _, err := save(0, []string{"10.0.0.1"}, 3); err != nil {
		t.Fatalf("第一次保存失败: %v", err)
	}
	got, err := save(1, []string{"192.168.001.*", "10.0.0.1"}, 7)
	if err != nil {
		t.Fatalf("第二次保存失败（OnConflict 冲突目标错误的历史 bug）: %v", err)
	}
	if got.InstanceType != 1 || got.MaxPlayerCount != 7 {
		t.Fatalf("运行时实例未更新: type=%d max=%d", got.InstanceType, got.MaxPlayerCount)
	}
	if len(got.Whitelist) != 2 || got.Whitelist[0] != "192.168.1.*" || got.Whitelist[1] != "10.0.0.1" {
		t.Fatalf("白名单规范化/去重不符合预期: %v", got.Whitelist)
	}

	// 持久化行必须与运行时一致（客户端重连/服务端重启后按 SID 合并恢复）
	var settings model.InstanceSettings
	if err := global.STORAGE_DB.Where("s_id = ?", "sid-1").First(&settings).Error; err != nil {
		t.Fatalf("读取持久化行失败: %v", err)
	}
	if settings.InstanceType != 1 || settings.MaxPlayerCount != 7 ||
		len(settings.Whitelist) != 2 || settings.Whitelist[0] != "192.168.1.*" {
		t.Fatalf("持久化行与保存值不一致: %+v", settings)
	}

	// 非法通配网段（不完整 4 段）应拒绝且不产生副作用
	before := settings
	if _, err := save(0, []string{"192.168.*"}, -1); err == nil {
		t.Fatal("非法网段 192.168.* 应当被拒绝")
	}
	if err := global.STORAGE_DB.Where("s_id = ?", "sid-1").First(&settings).Error; err != nil {
		t.Fatal(err)
	}
	if settings.InstanceType != before.InstanceType || settings.MaxPlayerCount != before.MaxPlayerCount ||
		len(settings.Whitelist) != len(before.Whitelist) {
		t.Fatal("校验失败后持久化行不应被改动")
	}
}

func mustOpenMemory(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	return db
}

func mustOpenTemp(t *testing.T) *gorm.DB {
	t.Helper()
	path := os.TempDir() + "/thingue_upsert_test.db"
	os.Remove(path)
	t.Cleanup(func() { os.Remove(path) })
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	return db
}
