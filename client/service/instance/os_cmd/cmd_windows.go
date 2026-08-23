package os_cmd

import (
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func SetCmd(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true}
	cmd.Stdout = os.Stdout
	return cmd
}

// PrepareCmd 在启动实例进程前调用。Windows 通过 Job Object 管理进程树，
// 此处无需额外设置，保持现有窗口行为。
func PrepareCmd(cmd *exec.Cmd) {
}

// ProcessGroup 封装一个 Windows Job Object，用于按句柄可靠终止整棵进程树。
type ProcessGroup struct {
	handle windows.Handle
}

// Capture 创建 Job Object 并将 pid 对应进程纳入其中。设置
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE 后，进程派生的所有后代进程都会归入此 Job，
// 终止 Job 即可一次性带走整棵进程树。
func Capture(pid int) (*ProcessGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(handle)
	if err = windows.AssignProcessToJobObject(job, handle); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &ProcessGroup{handle: job}, nil
}

// Kill 终止 Job 内的全部进程并释放句柄。按 Job 句柄终止，
// 即使 pid 已被系统回收复用也不会误杀其它进程。
func (g *ProcessGroup) Kill() error {
	err := windows.TerminateJobObject(g.handle, 1)
	windows.CloseHandle(g.handle)
	return err
}
