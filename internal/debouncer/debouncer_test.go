package debouncer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ItsOrganic/whack/internal/watcher"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func recvSignal(t *testing.T, out <-chan BuildSignal, timeout time.Duration) (BuildSignal, bool) {
	t.Helper()
	select {
	case sig, ok := <-out:
		return sig, ok
	case <-time.After(timeout):
		return BuildSignal{}, false
	}
}

func TestDebouncer_CoalescesBurstIntoOneSignal(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	writeFile(t, f, "package main\n")

	d := New(Options{Delay: 30 * time.Millisecond, WatchDirs: []string{dir}, Includes: []string{"*.go"}, WorkingDir: dir})
	if err := d.PrimeFingerprint(); err != nil {
		t.Fatalf("PrimeFingerprint: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan watcher.FileEvent, 8)
	out := d.Start(ctx, in)

	writeFile(t, f, "package main\n// v2\n")
	in <- watcher.FileEvent{Path: f, EventType: "write", Time: time.Now()}
	in <- watcher.FileEvent{Path: f, EventType: "write", Time: time.Now()}
	in <- watcher.FileEvent{Path: f, EventType: "write", Time: time.Now()}

	sig, ok := recvSignal(t, out, 500*time.Millisecond)
	if !ok {
		t.Fatal("expected exactly one coalesced signal, got none")
	}
	if len(sig.Events) != 3 {
		t.Errorf("expected the signal to carry all 3 coalesced events, got %d", len(sig.Events))
	}

	if _, ok := recvSignal(t, out, 100*time.Millisecond); ok {
		t.Fatal("expected only one signal for one burst, got a second")
	}
}

func TestDebouncer_NoOpEditDoesNotSignal(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	writeFile(t, f, "package main\n")

	d := New(Options{Delay: 20 * time.Millisecond, WatchDirs: []string{dir}, Includes: []string{"*.go"}, WorkingDir: dir})
	if err := d.PrimeFingerprint(); err != nil {
		t.Fatalf("PrimeFingerprint: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan watcher.FileEvent, 8)
	out := d.Start(ctx, in)

	// An event with no actual filesystem change behind it (e.g. an editor
	// touching a file without altering it) must not trigger a rebuild.
	in <- watcher.FileEvent{Path: f, EventType: "write", Time: time.Now()}

	if _, ok := recvSignal(t, out, 150*time.Millisecond); ok {
		t.Fatal("a no-op event must not produce a build signal")
	}
}

func TestDebouncer_IncrementalUpdateDetectsRealChange(t *testing.T) {
	// Verifies the incremental (event-driven) fingerprint update - not just
	// the initial full-walk prime - correctly detects a real content change
	// on the specific file an event names.
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	writeFile(t, f, "package main\n")

	d := New(Options{Delay: 20 * time.Millisecond, WatchDirs: []string{dir}, Includes: []string{"*.go"}, WorkingDir: dir})
	if err := d.PrimeFingerprint(); err != nil {
		t.Fatalf("PrimeFingerprint: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan watcher.FileEvent, 8)
	out := d.Start(ctx, in)

	// Give the new content a distinct size so it's unambiguous even on
	// filesystems with coarse mtime resolution.
	writeFile(t, f, "package main\n\nfunc main() {}\n")
	in <- watcher.FileEvent{Path: f, EventType: "write", Time: time.Now()}

	if _, ok := recvSignal(t, out, 500*time.Millisecond); !ok {
		t.Fatal("expected a signal for a genuine content change")
	}
}

func TestDebouncer_RemoveEventDropsFileFromIndex(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	writeFile(t, f, "package main\n")

	d := New(Options{Delay: 20 * time.Millisecond, WatchDirs: []string{dir}, Includes: []string{"*.go"}, WorkingDir: dir})
	if err := d.PrimeFingerprint(); err != nil {
		t.Fatalf("PrimeFingerprint: %v", err)
	}
	before := d.lastFingerprint

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan watcher.FileEvent, 8)
	out := d.Start(ctx, in)

	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	in <- watcher.FileEvent{Path: f, EventType: "remove", Time: time.Now()}

	sig, ok := recvSignal(t, out, 500*time.Millisecond)
	if !ok {
		t.Fatal("expected a signal for a file removal")
	}
	if sig.Fingerprint == before {
		t.Error("fingerprint should change once the file is removed from the index")
	}

	// Synchronize with the debouncer goroutine's exit before reading d.files
	// directly, to avoid racing with it.
	cancel()
	for range out {
	}
	if _, exists := d.files[f]; exists {
		t.Error("removed file should no longer be present in the incremental index")
	}
}

func TestDebouncer_BufferReplacesStaleSignalInsteadOfDroppingLatest(t *testing.T) {
	// Regression test for the fix this session: the build-signal channel has
	// a buffer of 1. If two distinct edits produce two signals before the
	// consumer reads either, the old code dropped whichever signal didn't
	// fit - which could permanently lose the *last* edit in a burst. The fix
	// makes the channel always hold the newest signal.
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	writeFile(t, f, "v0")

	d := New(Options{Delay: 10 * time.Millisecond, WatchDirs: []string{dir}, Includes: []string{"*.go"}, WorkingDir: dir})
	if err := d.PrimeFingerprint(); err != nil {
		t.Fatalf("PrimeFingerprint: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in := make(chan watcher.FileEvent, 8)
	out := d.Start(ctx, in) // consumer deliberately doesn't read yet

	writeFile(t, f, "v1 - slightly longer content")
	in <- watcher.FileEvent{Path: f, EventType: "write", Time: time.Now()}
	time.Sleep(60 * time.Millisecond) // let this tick fire and sit unread in the buffer

	writeFile(t, f, "v2 - final content, even longer than v1")
	in <- watcher.FileEvent{Path: f, EventType: "write", Time: time.Now()}
	time.Sleep(60 * time.Millisecond) // this tick must replace, not be dropped by, the pending one

	sig, ok := recvSignal(t, out, 500*time.Millisecond)
	if !ok {
		t.Fatal("expected a signal to be available")
	}
	// A second read should find nothing else pending (only one slot).
	if _, ok := recvSignal(t, out, 100*time.Millisecond); ok {
		t.Error("only one signal should have been buffered")
	}

	final, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(final) != "v2 - final content, even longer than v1" {
		t.Fatalf("test setup invariant broken: %s", final)
	}

	// Synchronize with the debouncer goroutine's exit (via the closed `out`
	// channel) before touching d.files/d.computeFingerprint from this
	// goroutine, to avoid racing with it.
	cancel()
	for range out {
	}

	expected := d.computeFingerprint()
	if sig.Fingerprint != expected {
		t.Fatal("delivered signal's fingerprint does not reflect the final (v2) state - the later edit may have been silently dropped instead of replacing the pending one")
	}
}
