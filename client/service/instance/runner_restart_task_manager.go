package instance

import (
	"fmt"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/provider"

	"github.com/robfig/cron/v3"
)

type RunnerRestartTaskManager struct {
	restartCron        *cron.Cron
	restartTaskEntryID cron.EntryID
}

func (t *RunnerRestartTaskManager) Init(restartTaskCron string) {
	t.restartCron = cron.New()
	if provider.AppConfig.SystemSettings.EnableRestartTask || restartTaskCron != "" {
		err := t.Start(restartTaskCron)
		if err != nil {
			// 如果开启失败将设置改为false
			provider.AppConfig.SystemSettings.EnableRestartTask = false
			provider.WriteConfigToFile()
		}
	}
}

func (t *RunnerRestartTaskManager) Start(restartTaskCron string) error {
	var err error
	appConfig := provider.AppConfig
	taskCron := appConfig.SystemSettings.RestartTaskCron
	if restartTaskCron != "" {
		taskCron = restartTaskCron
		logger.Zap.Debug(fmt.Printf("启动定时任务: %s", taskCron))
	}
	t.restartTaskEntryID, err = t.restartCron.AddFunc(taskCron, func() {
		logger.Zap.Debug("重启定时任务执行开始")
		RunnerManager.RestartAllRunner()
		logger.Zap.Debug("重启定时任务执行结束")
	})
	t.restartCron.Start()
	return err
}

func (t *RunnerRestartTaskManager) Stop() {
	t.restartCron.Remove(t.restartTaskEntryID)
	t.restartCron.Stop()
}
