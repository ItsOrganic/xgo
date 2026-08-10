// Linux-only by design (see the design spec's "Out of scope" section):
// process-group signaling via syscall.Kill(-pgid, ...) and resource sampling
// via proctree.go's /proc parsing are both Linux-specific.
package harness

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var readyLineRegex = regexp.MustCompile(`READY build=(\d+) ts=\d+`)

// ToolSpec describes how to launch one hot-reload tool against one
// scenario. NewCmd must return a fresh, unstarted *exec.Cmd on every call -
// an exec.Cmd cannot be reused once Wait has been called on it, and each
// trial batch starts the tool anew.
type ToolSpec struct {
	Name   string
	NewCmd func() *exec.Cmd
}

type readyEvent struct {
	build int
	at    time.Time
}

// runningTool wraps one started tool subprocess: a background goroutine
// scans its combined stdout/stderr for READY lines, another waits for exit.
type runningTool struct {
	cmd         *exec.Cmd
	startedAt   time.Time
	readyCh     chan readyEvent
	exitedCh    chan struct{}
	exitErr     error
	recentLines *ringBuffer
}

func startTool(spec ToolSpec) (*runningTool, error) {
	cmd := spec.NewCmd()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create output pipe: %w", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	rt := &runningTool{
		readyCh:     make(chan readyEvent, 64),
		exitedCh:    make(chan struct{}),
		recentLines: newRingBuffer(50),
	}

	rt.startedAt = time.Now()
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, fmt.Errorf("start %s: %w", spec.Name, err)
	}
	pw.Close() // our copy of the write end; the child holds its own
	rt.cmd = cmd

	go rt.scanOutput(pr)
	go func() {
		err := cmd.Wait()
		rt.exitErr = err
		close(rt.exitedCh) // happens-after the exitErr write, per the Go memory model
	}()

	return rt, nil
}

func (rt *runningTool) scanOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		rt.recentLines.add(line)
		if m := readyLineRegex.FindStringSubmatch(line); m != nil {
			if build, err := strconv.Atoi(m[1]); err == nil {
				select {
				case rt.readyCh <- readyEvent{build: build, at: time.Now()}:
				default: // 64-deep buffer should never fill in practice; drop rather than block scanning
				}
			}
		}
	}
}

// waitForReady blocks until a READY line for the given build number is
// seen, the process exits, or timeout elapses - whichever comes first.
func (rt *runningTool) waitForReady(build int, timeout time.Duration) (time.Time, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev := <-rt.readyCh:
			if ev.build == build {
				return ev.at, nil
			}
			// A stale ready line for an earlier build we already consumed,
			// or (shouldn't happen given sequential edits) one from the
			// future - either way, keep waiting for the one we asked for.
		case <-rt.exitedCh:
			return time.Time{}, fmt.Errorf("process exited unexpectedly (err=%v); recent output:\n%s", rt.exitErr, rt.recentLines.String())
		case <-timer.C:
			return time.Time{}, fmt.Errorf("timeout after %s waiting for READY build=%d; recent output:\n%s", timeout, build, rt.recentLines.String())
		}
	}
}

// stop signals the tool's entire process group (SIGTERM, then SIGKILL after
// timeout) and waits for the whole group to actually be gone.
//
// cmd.Wait() only confirms the direct child (the tool's own top-level
// process) has exited - it says nothing about descendants signaled via the
// same -pgid broadcast, since a process group signal doesn't terminate
// every member atomically. A grandchild can still be mid-exit after the
// direct child has already been reaped. So after the direct child exits,
// this also polls ProcessesInGroup until the group is empty (escalating to
// SIGKILL on stragglers), which is what the benchmark actually needs: a
// clean slate before the next trial starts, with no leftover process from
// this one still holding a port or burning CPU during the next sample.
func (rt *runningTool) stop(timeout time.Duration) {
	if rt.cmd == nil || rt.cmd.Process == nil {
		return
	}
	pgid := rt.cmd.Process.Pid // the tool is its own group leader (Setpgid, no explicit Pgid)
	deadline := time.Now().Add(timeout)

	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-rt.exitedCh:
	case <-time.After(time.Until(deadline)):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-rt.exitedCh
	}

	for time.Now().Before(deadline) {
		if len(ProcessesInGroup(pgid)) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stragglers := ProcessesInGroup(pgid); len(stragglers) > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		for i := 0; i < 25 && len(ProcessesInGroup(pgid)) > 0; i++ {
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// WriteMarker rewrites the scenario's marker.go so BuildMarker == n. The
// content shape is identical across both test app scenarios.
func WriteMarker(path string, n int) error {
	content := fmt.Sprintf(
		"package main\n\n// BuildMarker is rewritten by the benchmark harness before each simulated\n// edit.\nvar BuildMarker = %d\n",
		n,
	)
	return os.WriteFile(path, []byte(content), 0o644)
}

// MeasureColdStart runs `trials` full launch-to-first-ready cycles,
// returning per-trial latencies in milliseconds and a count of trials that
// failed to become ready within timeout (recorded, not silently dropped).
func MeasureColdStart(spec ToolSpec, trials int, timeout time.Duration) (rawMs []float64, failures int) {
	for i := 0; i < trials; i++ {
		rt, err := startTool(spec)
		if err != nil {
			failures++
			continue
		}
		readyTs, err := rt.waitForReady(0, timeout)
		rt.stop(3 * time.Second)
		if err != nil {
			failures++
			continue
		}
		rawMs = append(rawMs, readyTs.Sub(rt.startedAt).Seconds()*1000)
		time.Sleep(300 * time.Millisecond) // let the OS release the port/fds before the next launch
	}
	return rawMs, failures
}

// RebuildLatencyResult is the outcome of one steady-state edit-loop run.
type RebuildLatencyResult struct {
	RawMs    []float64
	Failures int
	Resource ResourceResult
}

// MeasureRebuildLatency starts the tool once, confirms the initial build
// comes up (discarded - that's cold start, measured separately), then
// performs `trials` simulated edits via markerPath, timing edit-to-ready
// for each while concurrently sampling the tool's whole process tree's
// resource usage.
func MeasureRebuildLatency(spec ToolSpec, markerPath string, trials int, editGap, readyTimeout, sampleInterval time.Duration) (RebuildLatencyResult, error) {
	if err := WriteMarker(markerPath, 0); err != nil {
		return RebuildLatencyResult{}, fmt.Errorf("reset marker: %w", err)
	}

	rt, err := startTool(spec)
	if err != nil {
		return RebuildLatencyResult{}, fmt.Errorf("start %s: %w", spec.Name, err)
	}
	defer rt.stop(3 * time.Second)

	if _, err := rt.waitForReady(0, readyTimeout); err != nil {
		return RebuildLatencyResult{}, fmt.Errorf("initial build never became ready: %w", err)
	}

	sampler := NewResourceSampler(rt.cmd.Process.Pid, sampleInterval)
	sampler.Start()

	result := RebuildLatencyResult{}
	for i := 1; i <= trials; i++ {
		if err := WriteMarker(markerPath, i); err != nil {
			result.Failures++
			continue
		}
		editTs := time.Now()
		readyTs, err := rt.waitForReady(i, readyTimeout)
		if err != nil {
			result.Failures++
		} else {
			result.RawMs = append(result.RawMs, readyTs.Sub(editTs).Seconds()*1000)
		}
		time.Sleep(editGap)
	}

	result.Resource = sampler.Stop()
	return result, nil
}

// ringBuffer keeps the last `size` lines added, for including recent tool
// output in timeout/crash error messages.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []string
	size int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{size: size}
}

func (r *ringBuffer) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, line)
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
	}
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.buf, "\n")
}
