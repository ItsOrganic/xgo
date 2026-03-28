//go:build !windows

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

func isWindows() bool { return false }

func applyProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processGroupID(cmd *exec.Cmd) int {
	if cmd.Process == nil {
		return 0
	}
	return cmd.Process.Pid
}

func sendTerminate(proc *os.Process, groupID int) error {
	if groupID > 0 {
		return syscall.Kill(-groupID, syscall.SIGTERM)
	}
	return proc.Signal(syscall.SIGTERM)
}

func forceKill(proc *os.Process, groupID int) error {
	if groupID > 0 {
		return syscall.Kill(-groupID, syscall.SIGKILL)
	}
	return proc.Kill()
}
