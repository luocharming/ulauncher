package api

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"thingue-launcher/client/service"
	"thingue-launcher/common/domain"
	"thingue-launcher/common/provider"
	coreservice "thingue-launcher/server/core/service"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type systemApi struct {
	ctx context.Context
}

var SystemApi = new(systemApi)

func (a *systemApi) Init(ctx context.Context) {
	a.ctx = ctx
	service.RunnerRestartTaskManager.Init("")
}

func (a *systemApi) OpenFileDialog(title string, displayName string, pattern string) (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
		Filters: []runtime.FileFilter{
			{
				DisplayName: displayName,
				Pattern:     pattern,
			},
		},
	})
}

func (a *systemApi) OpenExplorer(path string) error {
	var cmd *exec.Cmd
	if goruntime.GOOS == "windows" {
		cmd = exec.Command("explorer", filepath.Dir(path))
	} else if goruntime.GOOS == "linux" {
		cmd = exec.Command("open", filepath.Dir(path))
	}
	err := cmd.Run()
	return err
}

func (a *systemApi) GetAppConfig() *provider.Config {
	return provider.AppConfig
}

func (a *systemApi) ControlRestartTask(enable bool) error {
	var err error
	if enable {
		err = service.RunnerRestartTaskManager.Start("")
	} else {
		service.RunnerRestartTaskManager.Stop()
	}
	if err == nil {
		provider.AppConfig.SystemSettings.EnableRestartTask = enable
		provider.WriteConfigToFile()
	}
	return err
}

func (a *systemApi) UpdateSystemSettings(systemSettings provider.SystemSettings) {
	appConfig := provider.AppConfig
	appConfig.SystemSettings = systemSettings
	provider.WriteConfigToFile()
}

func (a *systemApi) GetVersionInfo() *domain.VersionInfo {
	return provider.VersionInfo
}

type LicenseInfo struct {
	ApplicationCode string  `json:"applicationCode"`
	Valid           bool    `json:"valid"`
	ExpireDate      string  `json:"expireDate"`
	RemainingDays   float64 `json:"remainingDays"`
	Message         string  `json:"message"`
}

func (a *systemApi) GetLicenseInfo() LicenseInfo {
	info := LicenseInfo{
		ApplicationCode: coreservice.LicenseService.GenerateApplicationCode(),
	}
	status, err := coreservice.LicenseService.LoadAndVerify(coreservice.LicenseService.LicensePath())
	if err != nil {
		info.Message = err.Error()
		return info
	}
	if !status.Valid {
		info.Message = status.Reason
		return info
	}
	info.Valid = true
	info.ExpireDate = status.ExpiresAt.Format("2006-01-02")
	rem := status.ExpiresAt.Sub(time.Now().UTC()).Hours() / 24.0
	if rem < 0 {
		rem = 0
	}
	info.RemainingDays = math.Round(rem*10) / 10
	info.Message = fmt.Sprintf("授权有效，到期：%s", info.ExpireDate)
	return info
}

func (a *systemApi) UploadLicenseFile() (LicenseInfo, error) {
	srcPath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择授权文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "授权文件 (*.lic)", Pattern: "*.lic"},
		},
	})
	if err != nil {
		return LicenseInfo{}, err
	}
	if srcPath == "" {
		return LicenseInfo{}, fmt.Errorf("未选择文件")
	}

	dstPath := coreservice.LicenseService.LicensePath()
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return LicenseInfo{}, fmt.Errorf("创建目录失败：%s", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return LicenseInfo{}, fmt.Errorf("打开文件失败：%s", err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return LicenseInfo{}, fmt.Errorf("写入文件失败：%s", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return LicenseInfo{}, fmt.Errorf("复制文件失败：%s", err)
	}

	return a.GetLicenseInfo(), nil
}

func (a *systemApi) ActivateLicenseCode(code string) (LicenseInfo, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return LicenseInfo{}, fmt.Errorf("激活码不能为空")
	}

	dstPath := coreservice.LicenseService.LicensePath()
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return LicenseInfo{}, fmt.Errorf("创建目录失败：%s", err)
	}

	if err := os.WriteFile(dstPath, []byte(code), 0o644); err != nil {
		return LicenseInfo{}, fmt.Errorf("写入激活码失败：%s", err)
	}

	return a.GetLicenseInfo(), nil
}
