package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func recvEvent(t *testing.T, events <-chan FileEvent, timeout time.Duration) (FileEvent, bool) {
	t.Helper()
	select {
	case evt, ok := <-events:
		return evt, ok
	case <-time.After(timeout):
		return FileEvent{}, false
	}
}

func drain(events <-chan FileEvent, errs <-chan error, timeout time.Duration) {
	deadline := time.After(timeout)
	for {
		select {
		case <-events:
		case <-errs:
		case <-deadline:
			return
		}
	}
}

func TestNew_WatchesTheConfiguredDirectory(t *testing.T) {
	dir := t.TempDir()
	w, err := New(Options{Dirs: []string{"."}, Includes: []string{"*.go"}, OutputBinary: "tmp/xgo-app", WorkingDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs := w.Start(ctx)
	defer drain(events, errs, 50*time.Millisecond)

	got := w.WatchedDirs()
	if len(got) != 1 || got[0] != dir {
		t.Fatalf("expected exactly [%s] watched, got %v", dir, got)
	}
}

func TestNew_UnderPathContainingExcludedSubstring(t *testing.T) {
	// Regression test for the session's root-cause bug: a project living
	// under a path that happens to contain "tmp" as a substring (like the OS
	// temp dir itself, which is exactly where t.TempDir() lives) must still
	// be watched - the exclude check must be relative to the project root,
	// not the absolute path.
	dir := t.TempDir() // typically something like /tmp/TestXxx.../001
	w, err := New(Options{Dirs: []string{"."}, Includes: []string{"*.go"}, OutputBinary: "tmp/xgo-app", WorkingDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs := w.Start(ctx)
	defer drain(events, errs, 50*time.Millisecond)

	if got := w.WatchedDirs(); len(got) == 0 {
		t.Fatalf("expected the project root to be watched despite living under a path containing 'tmp'; watched=%v (dir=%s)", got, dir)
	}
}

func TestWatcher_DetectsEditAfterDirDeleteAndRecreate(t *testing.T) {
	// Regression test: a watched subdirectory that's deleted and later
	// recreated at the same path used to silently stop being watched
	// forever, because the stale entry in the dedup map blocked
	// re-registration.
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	w, err := New(Options{Dirs: []string{"."}, Includes: []string{"*.go"}, OutputBinary: "tmp/xgo-app", WorkingDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs := w.Start(ctx)
	go func() {
		for range errs {
		}
	}()

	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // let the Remove event be processed

	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // let tryAddDir register the recreated dir

	f := filepath.Join(sub, "foo.go")
	if err := os.WriteFile(f, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-events:
			if evt.Path == f {
				return // success: the recreated directory is being watched
			}
		case <-deadline:
			t.Fatal("no event observed for a file created in a deleted-and-recreated directory - it silently stopped being watched")
		}
	}
}
