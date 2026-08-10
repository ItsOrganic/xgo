package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// writeScript writes an executable shell script and returns its path.
// Mirrors the fake-subprocess testing pattern used by the main xgo module's
// own runner tests: a tiny shell script standing in for a real tool is
// faster and more hermetic than driving real xgo/wgo/air in unit tests.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return p
}

// fakeToolScript emulates the READY-line contract: it watches a marker file
// (written in the same "var BuildMarker = N" shape WriteMarker produces)
// and prints a new READY line whenever the value changes.
const fakeToolScript = `
last=""
while true; do
  cur=$(grep -o 'BuildMarker = [0-9]*' "$1" | grep -o '[0-9]*$')
  if [ "$cur" != "$last" ]; then
    echo "READY build=$cur ts=$(date +%s%N)"
    last="$cur"
  fi
  sleep 0.03
done
`

func newFakeToolSpec(t *testing.T, dir string) (spec ToolSpec, markerPath string) {
	t.Helper()
	markerPath = filepath.Join(dir, "marker.go")
	script := writeScript(t, dir, "fake-tool.sh", fakeToolScript)
	return ToolSpec{
		Name: "fake",
		NewCmd: func() *exec.Cmd {
			return exec.Command(script, markerPath)
		},
	}, markerPath
}

func TestStartTool_ParsesReadyLine(t *testing.T) {
	dir := t.TempDir()
	spec, marker := newFakeToolSpec(t, dir)
	if err := WriteMarker(marker, 0); err != nil {
		t.Fatal(err)
	}

	rt, err := startTool(spec)
	if err != nil {
		t.Fatalf("startTool: %v", err)
	}
	defer rt.stop(2 * time.Second)

	readyTs, err := rt.waitForReady(0, 2*time.Second)
	if err != nil {
		t.Fatalf("waitForReady: %v", err)
	}
	if readyTs.Before(rt.startedAt) {
		t.Errorf("ready timestamp %v is before start timestamp %v", readyTs, rt.startedAt)
	}
}

func TestWaitForReady_DetectsCrash(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "crash.sh", "exit 3\n")
	spec := ToolSpec{Name: "crasher", NewCmd: func() *exec.Cmd { return exec.Command(script) }}

	rt, err := startTool(spec)
	if err != nil {
		t.Fatalf("startTool: %v", err)
	}
	defer rt.stop(2 * time.Second)

	start := time.Now()
	_, err = rt.waitForReady(0, 5*time.Second)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error for a process that exits without ever becoming ready")
	}
	if elapsed > 2*time.Second {
		t.Errorf("crash should be detected almost immediately via exitedCh, not by waiting out the full timeout; took %s", elapsed)
	}
}

func TestStop_KillsWholeProcessTree(t *testing.T) {
	dir := t.TempDir()
	childPidFile := filepath.Join(dir, "child.pid")
	script := writeScript(t, dir, "spawner.sh", `
sh -c 'echo $$ > "`+childPidFile+`"; while true; do sleep 1; done' &
echo "READY build=0 ts=$(date +%s%N)"
while true; do sleep 1; done
`)
	spec := ToolSpec{Name: "spawner", NewCmd: func() *exec.Cmd { return exec.Command(script) }}

	rt, err := startTool(spec)
	if err != nil {
		t.Fatalf("startTool: %v", err)
	}
	if _, err := rt.waitForReady(0, 2*time.Second); err != nil {
		t.Fatalf("waitForReady: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var childPID string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childPidFile)
		if err == nil && len(data) > 0 {
			childPID = string(data)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == "" {
		t.Fatal("spawned child never wrote its pid file")
	}

	rt.stop(2 * time.Second)

	// The grandchild (spawned via `sh -c ... &`, inheriting the tool's
	// process group) must be gone too, not just the direct child - this is
	// the whole point of killing by -pgid instead of just the tool's own pid.
	pid := string(childPID)
	pid = pid[:len(pid)-1] // trim trailing newline
	if _, err := os.Stat("/proc/" + pid); err == nil {
		t.Errorf("grandchild process %s is still alive after stop()", pid)
	}
}

func TestMeasureColdStart_RecordsLatenciesAndFailures(t *testing.T) {
	dir := t.TempDir()
	spec, marker := newFakeToolSpec(t, dir)
	if err := WriteMarker(marker, 0); err != nil {
		t.Fatal(err)
	}

	rawMs, failures := MeasureColdStart(spec, 3, 2*time.Second)
	if failures != 0 {
		t.Errorf("expected 0 failures for a well-behaved fake tool, got %d", failures)
	}
	if len(rawMs) != 3 {
		t.Fatalf("expected 3 latency samples, got %d", len(rawMs))
	}
	for i, v := range rawMs {
		if v < 0 {
			t.Errorf("latency[%d] = %v, should never be negative", i, v)
		}
	}
}

func TestMeasureColdStart_NeverReadyIsRecordedAsFailure(t *testing.T) {
	dir := t.TempDir()
	// A tool that starts but never prints anything - simulates a tool
	// hanging during its first build.
	script := writeScript(t, dir, "silent.sh", "while true; do sleep 1; done\n")
	spec := ToolSpec{Name: "silent", NewCmd: func() *exec.Cmd { return exec.Command(script) }}

	rawMs, failures := MeasureColdStart(spec, 1, 300*time.Millisecond)
	if failures != 1 {
		t.Errorf("expected 1 failure for a tool that never becomes ready, got %d", failures)
	}
	if len(rawMs) != 0 {
		t.Errorf("a failed trial must not contribute a latency sample, got %v", rawMs)
	}
}

func TestMeasureRebuildLatency_TracksEditsAndSamplesResources(t *testing.T) {
	dir := t.TempDir()
	spec, marker := newFakeToolSpec(t, dir)

	result, err := MeasureRebuildLatency(spec, marker, 4, 150*time.Millisecond, 2*time.Second, 30*time.Millisecond)
	if err != nil {
		t.Fatalf("MeasureRebuildLatency: %v", err)
	}
	if result.Failures != 0 {
		t.Errorf("expected 0 failures, got %d", result.Failures)
	}
	if len(result.RawMs) != 4 {
		t.Fatalf("expected 4 latency samples (one per edit), got %d: %v", len(result.RawMs), result.RawMs)
	}
	if result.Resource.AvgRSSKiB <= 0 {
		t.Errorf("expected non-zero resource sampling during the edit loop, got %+v", result.Resource)
	}
}
