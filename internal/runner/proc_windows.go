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
