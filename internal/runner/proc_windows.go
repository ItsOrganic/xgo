//go:build windows

package runner

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func isWindows() bool { return true }

var procGenerateConsoleCtrlEvent = syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")

// setVerbatimCmdLine hands cmd.exe its command line as-is. By default Go
// quotes each argument for the C runtime's parser, turning `"` into `\"`,
// which cmd.exe doesn't understand: the default build command's
// -ldflags="-s -w" would reach go.exe split into `-ldflags="-s` and `-w"`.
// With /S, cmd.exe strips just the outer quotes and runs the rest unchanged.
func setVerbatimCmdLine(cmd *exec.Cmd, line string) {
	sysProcAttr(cmd).CmdLine = line
}

func sysProcAttr(cmd *exec.Cmd) *syscall.SysProcAttr {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	return cmd.SysProcAttr
}

// applyProcessGroup starts the command in its own console process group, the
// closest Windows has to a Unix process group: a Ctrl+Break sent to that
// group reaches the cmd.exe wrapper and the app under it, but not xgo.
//
// It also replaces exec's default context cancellation, which would
// TerminateProcess only the cmd.exe wrapper and orphan the app under it.
// Sending Ctrl+Break instead leaves terminateProcess, which runs on every
// shutdown, to wait for a graceful exit and force-kill the tree on timeout.
func applyProcessGroup(cmd *exec.Cmd) {
	sysProcAttr(cmd).CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
	cmd.Cancel = func() error {
		_ = sendCtrlBreak(cmd.Process.Pid)
		return nil
	}
}

// processGroupID returns the group's ID, which is its leader's PID.
func processGroupID(cmd *exec.Cmd) int {
	if cmd.Process == nil {
		return 0
	}
	return cmd.Process.Pid
}

// sendTerminate asks the process group to exit with Ctrl+Break, which a Go
// app receives as os.Interrupt. (*os.Process).Signal can't do this: on
// Windows it only supports os.Kill. Without a shared console (xgo started
// detached) there is no way to ask, so it falls back to a hard kill.
func sendTerminate(proc *os.Process, groupID int) error {
	if groupID > 0 && sendCtrlBreak(groupID) == nil {
		return nil
	}
	return forceKill(proc, groupID)
}

func sendCtrlBreak(groupID int) error {
	r, _, err := procGenerateConsoleCtrlEvent.Call(uintptr(syscall.CTRL_BREAK_EVENT), uintptr(groupID))
	if r == 0 {
		return err
	}
	return nil
}

// forceKill kills the whole process tree: proc is the cmd.exe wrapper, and
// killing only it would leave the app running and holding its port.
func forceKill(proc *os.Process, groupID int) error {
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(proc.Pid)).Run(); err != nil {
		return proc.Kill()
	}
	return nil
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
