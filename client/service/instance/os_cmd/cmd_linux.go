package os_cmd

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func SetCmd(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdout = os.Stdout
	return cmd
}

// PrepareCmd 在启动实例进程前调用。设置 Setpgid 让子进程成为独立进程组的组长
// （pgid == pid），使其派生的所有后代进程归入同一进程组，便于整组终止。
func PrepareCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// ProcessGroup 封装一个进程组，用于按进程组可靠终止整棵进程树。
type ProcessGroup struct {
	pgid int
}

// Capture 记录进程组 id。因启动时已设置 Setpgid，组长 pid 即为 pgid。
func Capture(pid int) (*ProcessGroup, error) {
	return &ProcessGroup{pgid: pid}, nil
}

// Kill 先向整个进程组发送 SIGTERM，宽限后再发送 SIGKILL，
// 确保整棵进程树被终止。传入负 pgid 表示作用于整个进程组。
func (g *ProcessGroup) Kill() error {
	err := syscall.Kill(-g.pgid, syscall.SIGTERM)
	time.Sleep(2 * time.Second)
	syscall.Kill(-g.pgid, syscall.SIGKILL)
	return err
}
