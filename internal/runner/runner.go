package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ItsOrganic/xgo/internal/debouncer"
	"github.com/ItsOrganic/xgo/internal/logger"
)

// CommandSpec describes a command started by the runner.
type CommandSpec struct {
	Name string
	Cmd  string
	Args []string
	Env  []string
	Dir  string
}

// Config controls runner lifecycle.
type Config struct {
	BuildCmd      string
	BuildEnv      []string
	BuildTimeout  time.Duration
	BeforeHooks   []string
	AfterHooks    []string
	Main          CommandSpec
	Extra         []CommandSpec
	WorkingDir    string
	OutputBinary  string
	Logger        *logger.Logger
	SignalTimeout time.Duration
}

// StatusSnapshot captures runner runtime status.
type StatusSnapshot struct {
	PIDs            map[string]int `yaml:"pids"`
	RestartCount    int            `yaml:"restart_count"`
	LastRestartTime time.Time      `yaml:"last_restart_time"`
	LastBuild       time.Duration  `yaml:"last_build_duration"`
	LastBuildFailed bool           `yaml:"last_build_failed"`
}

type processInfo struct {
	spec     CommandSpec
	cmd      *exec.Cmd
	pid      int
	group    int
	done     chan struct{}
	waitErr  error
	stopping atomic.Bool
}

// Runner manages build/restart lifecycle.
type Runner struct {
	cfg Config

	mu              sync.Mutex
	procs           map[string]*processInfo
	restartCount    int
	lastRestartTime time.Time
	lastBuild       time.Duration
	lastBuildFailed bool
}

// New creates a runner.
func New(cfg Config) *Runner {
	if cfg.SignalTimeout <= 0 {
		cfg.SignalTimeout = 5 * time.Second
	}
	if cfg.BuildTimeout <= 0 {
		cfg.BuildTimeout = 30 * time.Second
	}
	return &Runner{cfg: cfg, procs: make(map[string]*processInfo)}
}

// Run listens for build signals and performs restart cycles.
func (r *Runner) Run(ctx context.Context, in <-chan debouncer.BuildSignal) error {
	if err := r.rebuildAndRestart(ctx); err != nil {
		r.cfg.Logger.Errorf("initial build failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return r.stopAll(context.Background())
		case _, ok := <-in:
			if !ok {
				return r.stopAll(context.Background())
			}
			if err := r.rebuildAndRestart(ctx); err != nil {
				r.cfg.Logger.Warnf("rebuild failed: %v", err)
			}
		}
	}
}

// Snapshot returns a copy of the current status.
func (r *Runner) Snapshot() StatusSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	pids := make(map[string]int, len(r.procs))
	for name, p := range r.procs {
		pids[name] = p.pid
	}
	return StatusSnapshot{
		PIDs:            pids,
		RestartCount:    r.restartCount,
		LastRestartTime: r.lastRestartTime,
		LastBuild:       r.lastBuild,
		LastBuildFailed: r.lastBuildFailed,
	}
}

func (r *Runner) rebuildAndRestart(parentCtx context.Context) error {
	for _, hook := range r.cfg.BeforeHooks {
		if err := r.runShell(parentCtx, hook, nil, ""); err != nil {
			return fmt.Errorf("before hook failed: %w", err)
		}
	}

	start := time.Now()
	if err := r.runBuild(parentCtx); err != nil {
		r.mu.Lock()
		r.lastBuild = time.Since(start)
		r.lastBuildFailed = true
		r.mu.Unlock()
		return err
	}
	buildDuration := time.Since(start)

	if err := r.stopAll(parentCtx); err != nil {
		r.cfg.Logger.Warnf("stop old processes: %v", err)
	}
	if err := r.startAll(parentCtx); err != nil {
		return err
	}
	for _, hook := range r.cfg.AfterHooks {
		if err := r.runShell(parentCtx, hook, nil, ""); err != nil {
			r.cfg.Logger.Warnf("after hook failed: %v", err)
		}
	}

	snap := r.Snapshot()
	mainPID := snap.PIDs[r.cfg.Main.Name]
	r.cfg.Logger.Successf("restarted in %s (pid=%d, restart #%d)", buildDuration.Round(time.Millisecond), mainPID, snap.RestartCount)

	r.mu.Lock()
	r.lastBuild = buildDuration
	r.lastBuildFailed = false
	r.lastRestartTime = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *Runner) runBuild(parentCtx context.Context) error {
	ctx, cancel := context.WithTimeout(parentCtx, r.cfg.BuildTimeout)
	defer cancel()

	cmd := shellCommandContext(ctx, r.cfg.BuildCmd)
	cmd.Dir = r.cfg.WorkingDir
	cmd.Env = mergeEnv(os.Environ(), r.cfg.BuildEnv)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		r.cfg.Logger.BuildError(strings.TrimSpace(buf.String()))
		return fmt.Errorf("build command failed: %w", err)
	}
	if out := strings.TrimSpace(buf.String()); out != "" {
		r.cfg.Logger.Infof("build output:\n%s", out)
	}
	return nil
}

func (r *Runner) startAll(ctx context.Context) error {
	cmds := []CommandSpec{r.cfg.Main}
	cmds = append(cmds, r.cfg.Extra...)
	for _, spec := range cmds {
		if strings.TrimSpace(spec.Cmd) == "" {
			continue
		}
		proc, err := r.startCmd(ctx, spec)
		if err != nil {
			return fmt.Errorf("start %s: %w", spec.Name, err)
		}
		r.mu.Lock()
		r.procs[spec.Name] = proc
		r.mu.Unlock()
		r.cfg.Logger.Infof("started %s pid=%d", spec.Name, proc.pid)
	}
	r.mu.Lock()
	r.restartCount++
	r.lastRestartTime = time.Now()
	r.mu.Unlock()
	return nil
}

func (r *Runner) stopAll(ctx context.Context) error {
	r.mu.Lock()
	procs := make([]*processInfo, 0, len(r.procs))
	for _, p := range r.procs {
		procs = append(procs, p)
	}
	r.procs = make(map[string]*processInfo)
	r.mu.Unlock()

	var allErr error
	for _, p := range procs {
		if err := terminateProcess(ctx, p, r.cfg.SignalTimeout); err != nil {
			allErr = errors.Join(allErr, fmt.Errorf("stop %s: %w", p.spec.Name, err))
		}
	}
	return allErr
}

func (r *Runner) startCmd(ctx context.Context, spec CommandSpec) (*processInfo, error) {
	command := shellCommandContext(ctx, withArgs(spec.Cmd, spec.Args))
	command.Stdin = os.Stdin

	dir := r.cfg.WorkingDir
	if spec.Dir != "" {
		if filepath.IsAbs(spec.Dir) {
			dir = spec.Dir
		} else {
			dir = filepath.Join(r.cfg.WorkingDir, spec.Dir)
		}
	}
	command.Dir = dir
	command.Env = mergeEnv(os.Environ(), spec.Env)
	applyProcessGroup(command)

	var (
		stdout io.ReadCloser
		stderr io.ReadCloser
		err    error
	)
	if spec.Name == r.cfg.Main.Name {
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
	} else {
		stdout, err = command.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("stdout pipe: %w", err)
		}
		stderr, err = command.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("stderr pipe: %w", err)
		}
	}

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	proc := &processInfo{spec: spec, cmd: command, pid: command.Process.Pid, group: processGroupID(command), done: make(chan struct{})}

	go func() {
		if spec.Name != r.cfg.Main.Name {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				r.forwardPrefixed(spec.Name, stdout)
			}()
			go func() {
				defer wg.Done()
				r.forwardPrefixed(spec.Name, stderr)
			}()
			wg.Wait()
		}
		proc.waitErr = command.Wait()
		close(proc.done)
		if !proc.stopping.Load() {
			r.handleUnexpectedExit(proc)
		}
	}()
	return proc, nil
}

func (r *Runner) handleUnexpectedExit(proc *processInfo) {
	r.mu.Lock()
	if cur, ok := r.procs[proc.spec.Name]; ok && cur.pid == proc.pid {
		delete(r.procs, proc.spec.Name)
	}
	r.mu.Unlock()
	if proc.waitErr != nil {
		r.cfg.Logger.Warnf("%s exited unexpectedly (pid=%d): %v", proc.spec.Name, proc.pid, proc.waitErr)
	} else {
		r.cfg.Logger.Warnf("%s exited unexpectedly (pid=%d)", proc.spec.Name, proc.pid)
	}
}

func (r *Runner) forwardPrefixed(name string, rd io.Reader) {
	scanner := bufio.NewScanner(rd)
	for scanner.Scan() {
		r.cfg.Logger.ExtraLine(name, scanner.Text())
	}
}

func (r *Runner) runShell(ctx context.Context, cmdStr string, env []string, dir string) error {
	cmd := shellCommandContext(ctx, cmdStr)
	if dir == "" {
		dir = r.cfg.WorkingDir
	}
	cmd.Dir = dir
	cmd.Env = mergeEnv(os.Environ(), env)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command %q: %w (%s)", cmdStr, err, strings.TrimSpace(string(out)))
	}
	trim := strings.TrimSpace(string(out))
	if trim != "" {
		r.cfg.Logger.Infof("%s", trim)
	}
	return nil
}

func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	index := map[string]int{}
	out := append([]string{}, base...)
	for i, kv := range out {
		parts := strings.SplitN(kv, "=", 2)
		index[parts[0]] = i
	}
	for _, kv := range extra {
		parts := strings.SplitN(kv, "=", 2)
		key := parts[0]
		if i, ok := index[key]; ok {
			out[i] = kv
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	return out
}

func shellCommandContext(ctx context.Context, raw string) *exec.Cmd {
	if isWindows() {
		return exec.CommandContext(ctx, "cmd", "/C", raw)
	}
	return exec.CommandContext(ctx, "sh", "-c", raw)
}

func withArgs(cmd string, args []string) string {
	if len(args) == 0 {
		return cmd
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, cmd)
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if isWindows() {
		return strconv.Quote(s)
	}
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func terminateProcess(ctx context.Context, p *processInfo, timeout time.Duration) error {
	if p == nil || p.cmd.Process == nil {
		return nil
	}
	p.stopping.Store(true)
	if err := sendTerminate(p.cmd.Process, p.group); err != nil {
		return err
	}

	select {
	case <-p.done:
		// The process exited in response to our own signal; whatever exit
		// status/signal it reports is expected, not a failure to surface.
		return nil
	case <-time.After(timeout):
		if err := forceKill(p.cmd.Process, p.group); err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		if err := forceKill(p.cmd.Process, p.group); err != nil {
			return err
		}
		return ctx.Err()
	}
}
