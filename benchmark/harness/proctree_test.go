package harness

import (
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestDescendants_FindsGrandchildren(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proctree relies on /proc, Linux-only")
	}

	// Outer sh forks a nested sh, which forks sleep - two levels deep, so
	// this specifically exercises that Descendants walks the whole tree
	// rather than stopping at direct children.
	cmd := exec.Command("sh", "-c", `sh -c 'sleep 5' & wait`)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(2 * time.Second)
	var desc []int
	for time.Now().Before(deadline) {
		desc = Descendants(cmd.Process.Pid)
		if len(desc) >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(desc) < 2 {
		t.Fatalf("expected at least 2 descendants (child sh + grandchild sleep), got %d: %v", len(desc), desc)
	}
}

func TestTreeSnapshot_NonZeroForRunningProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proctree relies on /proc, Linux-only")
	}

	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	time.Sleep(50 * time.Millisecond)

	rss, _ := TreeSnapshot(cmd.Process.Pid)
	if rss <= 0 {
		t.Errorf("expected non-zero RSS for a running process, got %d KiB", rss)
	}
}

func TestResourceSampler_CollectsSamples(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proctree relies on /proc, Linux-only")
	}

	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	sampler := NewResourceSampler(cmd.Process.Pid, 50*time.Millisecond)
	sampler.Start()
	time.Sleep(300 * time.Millisecond)
	result := sampler.Stop()

	if result.AvgRSSKiB <= 0 {
		t.Errorf("expected non-zero AvgRSSKiB, got %v", result.AvgRSSKiB)
	}
	if result.PeakRSSKiB < result.AvgRSSKiB {
		t.Errorf("peak (%v) should be >= avg (%v)", result.PeakRSSKiB, result.AvgRSSKiB)
	}
	if result.AvgCPUPercent < 0 {
		t.Errorf("AvgCPUPercent should never be negative, got %v", result.AvgCPUPercent)
	}
}

func TestProcessesInGroup_SurvivesReparenting(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proctree relies on /proc, Linux-only")
	}

	// Regression test for the bug TestStop_KillsWholeProcessTree caught:
	// once a process's original parent exits, it's reparented (typically to
	// init), so Descendants(originalParentPid) can no longer find it - but
	// its PGID is unchanged, so ProcessesInGroup must still find it.
	cmd := exec.Command("sh", "-c", `sh -c 'sleep 5' & wait`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pgid := cmd.Process.Pid
	defer func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(2 * time.Second)
	var grandchildPID int
	for time.Now().Before(deadline) {
		desc := Descendants(pgid)
		if len(desc) >= 2 {
			grandchildPID = desc[len(desc)-1]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if grandchildPID == 0 {
		t.Fatal("grandchild sleep process never appeared")
	}

	// Kill only the direct child (the outer sh), leaving the grandchild
	// (the inner "sleep 5") orphaned and reparented, then confirm
	// ProcessesInGroup still reports it.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(Descendants(pgid)) == 0 {
			break // reparenting has happened; Descendants(pgid) no longer finds the grandchild
		}
		time.Sleep(20 * time.Millisecond)
	}

	inGroup := ProcessesInGroup(pgid)
	found := false
	for _, p := range inGroup {
		if p == grandchildPID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ProcessesInGroup(%d) = %v, expected it to still include reparented grandchild %d", pgid, inGroup, grandchildPID)
	}
}

func TestResourceSampler_StopBlocksUntilGoroutineDone(t *testing.T) {
	// Regression-style test: Stop() must not return before the sampling
	// goroutine has actually observed stopCh and exited, otherwise a caller
	// that immediately kills the sampled process right after Stop() could
	// race with one last in-flight sample read.
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	sampler := NewResourceSampler(cmd.Process.Pid, 10*time.Millisecond)
	sampler.Start()
	time.Sleep(50 * time.Millisecond)
	sampler.Stop()

	select {
	case <-sampler.doneCh:
	default:
		t.Fatal("doneCh should be closed immediately after Stop() returns")
	}
}
