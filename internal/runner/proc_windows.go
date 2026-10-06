//go:build windows

package runner

import (
	"os"
	"os/exec"
)

func isWindows() bool { return true }

func applyProcessGroup(cmd *exec.Cmd) {}

func processGroupID(cmd *exec.Cmd) int { return 0 }

func sendTerminate(proc *os.Process, groupID int) error {
	return proc.Signal(os.Interrupt)
}

func forceKill(proc *os.Process, groupID int) error {
	return proc.Kill()
}

// IsAlive reports whether pid refers to a currently-running process. Used by
// `xgo status` to tell a live run apart from stale leftover state.
//
// Unlike Unix, os.FindProcess on Windows actually opens a handle to the
// process and fails if it doesn't exist, so a successful FindProcess is
// itself a reasonable (if not airtight re: PID reuse) liveness signal;
// signal(0)-style probing isn't available since (*os.Process).Signal only
// supports os.Kill on this platform.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}
