package instance

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"thingue-launcher/client/global"
	"thingue-launcher/client/service/instance/os_cmd"
	"thingue-launcher/common/domain"
	"thingue-launcher/common/logger"
	"thingue-launcher/common/provider"
	"thingue-launcher/common/util"
	"time"
)

type Runner struct {
	*domain.Instance
	ExitSignalChannel chan error `json:"-"`
	process           *os.Process
	group             *os_cmd.ProcessGroup
	faultCount        uint
}

func (r *Runner) Start() error {
	if r.process != nil {
		return errors.New("实例已在运行")
	}
	var launchArguments []string
	// 设置PixelStreamingURL
	sid, err := ClientService.GetInstanceSid(global.CLIENT_ID, r.CID)
	if err == nil {
		r.SID = sid
		wsUrl := util.HttpUrlToWsUrl(provider.AppConfig.ServerURL, "/ws/streamer")
		launchArguments = append(r.LaunchArguments, "-PixelStreamingURL="+wsUrl+"/"+r.SID)
	} else {
		return err
	}
	// 设置日志文件名称为实例名称
	if r.Name != "" {
		launchArguments = append(launchArguments, "LOG="+r.Name+".log")
	}
	// 运行前
	logger.Zap.Debug(r.ExecPath, launchArguments)
	command := exec.Command(r.ExecPath, launchArguments...)
	os_cmd.PrepareCmd(command)
	err = command.Start()
	if err != nil {
		return err
	}
	r.Pid = command.Process.Pid
	r.process = command.Process
	if group, captureErr := os_cmd.Capture(r.Pid); captureErr == nil {
		r.group = group
	} else {
		logger.Zap.Warnf("实例进程组捕获失败，停止时将回退命令方式 %s %s", r.Name, captureErr)
	}
	r.updateStateCode(1)
	r.LastStartAt = time.Now()
	RunnerManager.RunnerStatusUpdateChanel <- r.CID
	logger.Zap.Infof("实例启动 %s", r.Name)
	go func() {
		exitCode := command.Wait()
		r.Pid = 0
		r.process = nil
		r.group = nil
		r.LastStopAt = time.Now()
		r.StreamerConnected = false
		select {
		case r.ExitSignalChannel <- exitCode:
			r.updateStateCode(0)
			RunnerManager.RunnerStatusUpdateChanel <- r.CID
			logger.Zap.Debugf("退出码发送成功 %s", r.Name)
			r.faultCount = 0
		default:
			r.updateStateCode(-1)
			RunnerManager.RunnerUnexpectedExitChanel <- r.CID
			logger.Zap.Warnf("实例异常退出 %s %d", r.Name, r.faultCount)
			if r.FaultRecover && r.faultCount < 3 {
				time.Sleep(3 * time.Second)
				r.Start()
			}
			r.faultCount++
		}
	}()
	return nil
}

func (r *Runner) Stop() error {
	if r.StateCode != 1 {
		return errors.New("实例未在运行")
	}
	var err error
	if group := r.group; group != nil {
		err = group.Kill()
	} else {
		err = r.killByPid()
	}
	select {
	case exitStatus := <-r.ExitSignalChannel:
		logger.Zap.Infof("实例停止 %s %s", r.Name, exitStatus)
	case <-time.After(10 * time.Second):
		logger.Zap.Warnf("实例停止等待退出超时 %s", r.Name)
	}
	return err
}

// killByPid 在进程组捕获失败时的兜底方案，按 PID 尽力终止进程。
func (r *Runner) killByPid() error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(r.Pid))
	case "linux":
		cmd = exec.Command("kill", "-KILL", strconv.Itoa(r.Pid))
	default:
		return errors.New("不支持的系统")
	}
	os_cmd.SetCmd(cmd)
	return cmd.Start()
}

func (r *Runner) updateStateCode(stateCode int8) {
	r.StateCode = stateCode
	ClientService.SendProcessState(r.SID, stateCode, r.Pid)
}

func (r *Runner) OpenLog() error {
	file, err := getLogFile(r.Instance)
	if err == nil {
		var cmdName string
		if provider.AppConfig.SystemSettings.ExternalEditorPath == "" {
			cmdName = "code"
		} else {
			cmdName = provider.AppConfig.SystemSettings.ExternalEditorPath
		}
		cmd := exec.Command(cmdName, file)
		return cmd.Run()
	}
	return err
}
