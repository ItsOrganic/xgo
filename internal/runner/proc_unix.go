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

// IsAlive reports whether pid refers to a currently-running process. Used by
// `xgo status` to tell a live run apart from stale leftover state.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, os.FindProcess always succeeds regardless of whether pid
	// exists; signal 0 is the standard existence/permission probe that sends
	// nothing but still errors if the process is gone.
	return proc.Signal(syscall.Signal(0)) == nil
}
