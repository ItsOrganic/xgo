package runner

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ItsOrganic/xgo/internal/debouncer"
	"github.com/ItsOrganic/xgo/internal/logger"
)

// writeScript writes an executable shell script and returns its path. Tests
// use tiny shell scripts instead of real `go build`/app binaries so they run
// fast and deterministically.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh scripts; TestTerminateProcess_StopsRealApp covers Windows")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return p
}

func newTestRunner(t *testing.T, cfg Config) *Runner {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = logger.New("[test]", false, false)
	}
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = t.TempDir()
	}
	if cfg.SignalTimeout == 0 {
		cfg.SignalTimeout = 2 * time.Second
	}
	if cfg.BuildTimeout == 0 {
		cfg.BuildTimeout = 5 * time.Second
	}
	return New(cfg)
}

// longRunningScript starts, then blocks until it receives SIGTERM, at which
// point it exits cleanly - simulating a well-behaved long-running app.
const longRunningScript = "trap 'exit 0' TERM\nwhile true; do sleep 0.05; done\n"

func TestRebuildAndRestart_FailedBuildKeepsOldProcessRunning(t *testing.T) {
	dir := t.TempDir()
	appScript := writeScript(t, dir, "app.sh", longRunningScript)
	failFlag := filepath.Join(dir, "should_fail")
	buildScript := writeScript(t, dir, "build.sh", `
if [ -f "`+failFlag+`" ]; then
  exit 1
fi
exit 0
`)

	r := newTestRunner(t, Config{
		BuildCmd:   buildScript,
		Main:       CommandSpec{Name: "app", Cmd: appScript},
		WorkingDir: dir,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { _ = r.stopAll(context.Background()) }()

	if err := r.rebuildAndRestart(ctx); err != nil {
		t.Fatalf("initial build should succeed: %v", err)
	}
	firstPID := r.Snapshot().PIDs["app"]
	if firstPID == 0 {
		t.Fatal("expected app to be running after initial build")
	}

	if err := os.WriteFile(failFlag, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.rebuildAndRestart(ctx); err == nil {
		t.Fatal("expected the second build to fail")
	}

	secondPID := r.Snapshot().PIDs["app"]
	if secondPID != firstPID {
		t.Fatalf("a failed build must not touch the running process: pid changed %d -> %d", firstPID, secondPID)
	}
}

func TestStartCmd_CrashIsDetectedAndUntracked(t *testing.T) {
	dir := t.TempDir()
	appScript := writeScript(t, dir, "app.sh", "exit 7\n") // exits immediately on its own
	buildScript := writeScript(t, dir, "build.sh", "exit 0\n")

	r := newTestRunner(t, Config{
		BuildCmd:   buildScript,
		Main:       CommandSpec{Name: "app", Cmd: appScript},
		WorkingDir: dir,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.rebuildAndRestart(ctx); err != nil {
		t.Fatalf("build should succeed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := r.Snapshot().PIDs["app"]; !ok {
			return // crash was detected and the process was untracked
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a process that exited on its own was never removed from tracked state")
}

func TestRun_GracefulShutdownReturnsNilError(t *testing.T) {
	// Regression test: switching the process-lifecycle tracking to
	// exec.Cmd.Wait() briefly caused a normal SIGTERM shutdown to be
	// reported as a fake error ("signal: terminated"), which made Run
	// return a non-nil error on ordinary, successful shutdown.
	dir := t.TempDir()
	appScript := writeScript(t, dir, "app.sh", longRunningScript)
	buildScript := writeScript(t, dir, "build.sh", "exit 0\n")

	r := newTestRunner(t, Config{
		BuildCmd:   buildScript,
		Main:       CommandSpec{Name: "app", Cmd: appScript},
		WorkingDir: dir,
	})
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan debouncer.BuildSignal)

	errCh := make(chan error, 1)
	go func() { errCh <- r.Run(ctx, in) }()

	time.Sleep(300 * time.Millisecond) // let the initial build/start settle
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("graceful shutdown must not return an error, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after ctx cancellation - shutdown hung")
	}
}

func TestRun_ExtrasStartOnceAcrossMultipleRebuilds(t *testing.T) {
	// Regression test: extra_cmds used to be torn down and restarted on
	// every single main-app rebuild, defeating their purpose as persistent
	// parallel processes.
	dir := t.TempDir()
	marker := filepath.Join(dir, "extra_starts")
	extraScript := writeScript(t, dir, "extra.sh", `echo x >> "`+marker+`"
`+longRunningScript)
	appScript := writeScript(t, dir, "app.sh", longRunningScript)
	buildScript := writeScript(t, dir, "build.sh", "exit 0\n")

	r := newTestRunner(t, Config{
		BuildCmd:   buildScript,
		Main:       CommandSpec{Name: "app", Cmd: appScript},
		Extra:      []CommandSpec{{Name: "extra-1", Cmd: extraScript}},
		WorkingDir: dir,
	})
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan debouncer.BuildSignal, 1)

	errCh := make(chan error, 1)
	go func() { errCh <- r.Run(ctx, in) }()

	time.Sleep(250 * time.Millisecond)
	for i := 0; i < 3; i++ {
		in <- debouncer.BuildSignal{}
		time.Sleep(250 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("shutdown returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not shut down")
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("extra command never started: %v", err)
	}
	count := strings.Count(string(data), "x")
	if count != 1 {
		t.Fatalf("extra command should start exactly once across 3 main-app rebuilds, started %d times", count)
	}
}

func TestStartCmd_ArgsWithSpecialCharactersSurviveIntact(t *testing.T) {
	// Regression/characterization test for replacing hand-rolled shell
	// quoting with the shell's own "$@" mechanism: args containing spaces,
	// quotes, and glob characters must arrive at the child process exactly
	// as given, not word-split, glob-expanded, or corrupted.
	dir := t.TempDir()
	outFile := filepath.Join(dir, "args.txt")
	appScript := writeScript(t, dir, "app.sh", `for a in "$@"; do printf '%s\n' "$a"; done > "`+outFile+`"
`)

	r := newTestRunner(t, Config{
		Main:       CommandSpec{Name: "app", Cmd: appScript, Args: []string{"hello world", "it's a test", "star*glob", "$HOME"}},
		WorkingDir: dir,
	})

	proc, err := r.startCmd(context.Background(), r.cfg.Main)
	if err != nil {
		t.Fatalf("startCmd: %v", err)
	}
	select {
	case <-proc.done:
	case <-time.After(2 * time.Second):
		t.Fatal("app.sh did not exit in time")
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("app.sh did not produce output: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	want := []string{"hello world", "it's a test", "star*glob", "$HOME"}
	if len(got) != len(want) {
		t.Fatalf("got %d args, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestHelperApp is not a real test: TestTerminateProcess_StopsRealApp runs
// the test binary itself as a stand-in app, so stopping is exercised against
// a real Go program on every OS, Windows included, with no shell scripts.
func TestHelperApp(t *testing.T) {
	dir := os.Getenv("XGO_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process for TestTerminateProcess_StopsRealApp")
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	_ = os.WriteFile(filepath.Join(dir, "ready"), []byte(strconv.Itoa(os.Getpid())), 0o644)
	<-sig
	_ = os.WriteFile(filepath.Join(dir, "stopped"), nil, 0o644)
	os.Exit(0)
}

func TestTerminateProcess_StopsRealApp(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, Config{WorkingDir: dir})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p, err := r.startCmd(ctx, CommandSpec{
		Name: "app",
		Cmd:  shellQuote(os.Args[0]) + " -test.run=TestHelperApp",
		Env:  []string{"XGO_HELPER_DIR=" + dir},
	})
	if err != nil {
		t.Fatalf("start helper app: %v", err)
	}

	// The app runs under the shell wrapper, so its PID is not p.pid.
	var appPID int
	deadline := time.Now().Add(10 * time.Second)
	for appPID == 0 {
		if time.Now().After(deadline) {
			t.Fatal("helper app never became ready")
		}
		if b, err := os.ReadFile(filepath.Join(dir, "ready")); err == nil {
			appPID, _ = strconv.Atoi(string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := terminateProcess(ctx, p, 5*time.Second); err != nil {
		t.Fatalf("terminateProcess: %v", err)
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("shell wrapper still running after terminateProcess")
	}

	// The bug this guards against: only the wrapper died, and the app kept
	// running (and holding its port) alongside its replacement.
	deadline = time.Now().Add(5 * time.Second)
	for IsAlive(appPID) {
		if time.Now().After(deadline) {
			t.Fatalf("app (pid %d) outlived terminateProcess", appPID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(dir, "stopped")); err != nil {
		t.Error("app was killed instead of being asked to stop gracefully")
	}
}
