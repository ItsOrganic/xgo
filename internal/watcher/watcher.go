package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ItsOrganic/xgo/internal/pathmatch"
	"github.com/fsnotify/fsnotify"
)

// FileEvent is a normalized file change event emitted by Watcher.
type FileEvent struct {
	Path      string
	EventType string
	Time      time.Time
}

// Options controls watcher behavior.
type Options struct {
	Dirs         []string
	Includes     []string
	Excludes     []string
	Gitignore    []string
	Verbose      bool
	OutputBinary string
	WorkingDir   string
}

// Watcher recursively watches directories and emits filtered file events.
type Watcher struct {
	fsw             *fsnotify.Watcher
	dirs            []string
	matcher         *pathmatch.Matcher
	outputBinary    string
	workingDir      string
	mu              sync.Mutex
	watched         map[string]struct{}
	watchedSnapshot []string
}

// New creates a watcher.
func New(opts Options) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}
	wd := opts.WorkingDir
	if wd == "" {
		wd, err = os.Getwd()
		if err != nil {
			_ = fsw.Close()
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	autoExcludes := []string{"vendor/", ".git/", "tmp/", "node_modules/", "*.pb.go", "*_mock.go"}
	if opts.OutputBinary != "" {
		autoExcludes = append(autoExcludes, filepath.Base(opts.OutputBinary))
	}
	excludes := append(autoExcludes, opts.Excludes...)

	dirs := opts.Dirs
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	includes := opts.Includes
	if len(includes) == 0 {
		includes = []string{"*.go"}
	}

	w := &Watcher{
		fsw:          fsw,
		dirs:         dirs,
		matcher:      pathmatch.New(wd, includes, excludes, opts.Gitignore),
		outputBinary: opts.OutputBinary,
		workingDir:   wd,
		watched:      make(map[string]struct{}),
	}
	return w, nil
}

// Start begins recursive watching and emits relevant events.
func (w *Watcher) Start(ctx context.Context) (<-chan FileEvent, <-chan error) {
	events := make(chan FileEvent, 256)
	errs := make(chan error, 8)

	if err := w.addInitialDirs(errs); err != nil {
		errs <- err
	}
	if len(w.WatchedDirs()) == 0 {
		select {
		case errs <- fmt.Errorf("no directories are being watched (check watch/exclude patterns and filesystem permissions) - file changes will NOT trigger rebuilds"):
		default:
		}
	}

	go func() {
		// Defers run LIFO: register close(events)/close(errs)/fsw.Close()
		// first so the recover handler (registered last, runs first) can
		// still safely send on errs before it's closed.
		defer close(events)
		defer close(errs)
		defer w.fsw.Close()
		defer func() {
			if r := recover(); r != nil {
				select {
				case errs <- fmt.Errorf("recovered from panic in watcher event loop: %v", r):
				default:
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-w.fsw.Errors:
				if !ok {
					return
				}
				select {
				case errs <- fmt.Errorf("watcher error: %w", err):
				default:
				}
			case evt, ok := <-w.fsw.Events:
				if !ok {
					return
				}
				if evt.Op&fsnotify.Create == fsnotify.Create {
					w.tryAddDir(evt.Name, errs)
				}
				if evt.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					// A watched directory that's deleted (or renamed away)
					// invalidates the underlying OS watch. Forget it so a
					// later Create at the same path re-registers instead of
					// silently staying dark forever (the dedup check in
					// addWatch would otherwise skip it as "already watched").
					w.removeWatch(evt.Name)
				}
				if !w.shouldEmit(evt.Name, evt.Op) {
					continue
				}
				fileEvent := FileEvent{Path: evt.Name, EventType: opString(evt.Op), Time: time.Now()}
				select {
				case events <- fileEvent:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return events, errs
}

// WatchedDirs returns the current watched directories.
func (w *Watcher) WatchedDirs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.watchedSnapshot))
	copy(out, w.watchedSnapshot)
	return out
}

func (w *Watcher) addInitialDirs(errs chan<- error) error {
	for _, dir := range w.dirs {
		root := dir
		if !filepath.IsAbs(root) {
			root = filepath.Join(w.workingDir, root)
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				select {
				case errs <- fmt.Errorf("walk %s: %w", path, walkErr):
				default:
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if w.matcher.ExcludedDir(path) {
				return filepath.SkipDir
			}
			if err := w.addWatch(path); err != nil {
				select {
				case errs <- fmt.Errorf("watch %s: %w", path, err):
				default:
				}
				return nil
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walk watch dir %s: %w", root, err)
		}
	}
	return nil
}

func (w *Watcher) addWatch(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.watched[dir]; exists {
		return nil
	}
	if err := w.fsw.Add(dir); err != nil {
		return fmt.Errorf("add watch %s: %w", dir, err)
	}
	w.watched[dir] = struct{}{}
	w.watchedSnapshot = append(w.watchedSnapshot, dir)
	return nil
}

// removeWatch forgets a previously-watched directory. Safe to call for paths
// that were never watched (e.g. a deleted file rather than a directory) -
// it's then just a no-op map lookup.
func (w *Watcher) removeWatch(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.watched[dir]; !exists {
		return
	}
	delete(w.watched, dir)
	_ = w.fsw.Remove(dir) // best-effort; the OS watch is already invalid if the dir is gone
	out := w.watchedSnapshot[:0]
	for _, d := range w.watchedSnapshot {
		if d != dir {
			out = append(out, d)
		}
	}
	w.watchedSnapshot = out
}

func (w *Watcher) tryAddDir(path string, errs chan<- error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	if w.matcher.ExcludedDir(path) {
		return
	}
	if err := w.addWatch(path); err != nil {
		select {
		case errs <- fmt.Errorf("watch %s: %w", path, err):
		default:
		}
	}
}

func (w *Watcher) shouldEmit(path string, op fsnotify.Op) bool {
	if op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
		return false
	}
	if w.isExcluded(path) {
		return false
	}
	return w.matcher.Included(path)
}

// isExcluded layers the output-binary exclusion (watcher-specific: we never
// want a rebuild triggered by writing our own compiled output) on top of the
// shared include/exclude pattern matcher.
func (w *Watcher) isExcluded(path string) bool {
	rel, err := filepath.Rel(w.workingDir, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || rel == "." {
		rel = filepath.ToSlash(path)
	}
	base := filepath.Base(path)
	if rel == filepath.ToSlash(w.outputBinary) || base == filepath.Base(w.outputBinary) {
		return true
	}
	return w.matcher.ExcludedFile(path)
}

func opString(op fsnotify.Op) string {
	switch {
	case op&fsnotify.Create == fsnotify.Create:
		return "create"
	case op&fsnotify.Write == fsnotify.Write:
		return "write"
	case op&fsnotify.Remove == fsnotify.Remove:
		return "remove"
	case op&fsnotify.Rename == fsnotify.Rename:
		return "rename"
	case op&fsnotify.Chmod == fsnotify.Chmod:
		return "chmod"
	default:
		return "unknown"
	}
}
