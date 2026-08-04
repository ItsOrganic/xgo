package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	Verbose      bool
	OutputBinary string
	WorkingDir   string
}

// Watcher recursively watches directories and emits filtered file events.
type Watcher struct {
	fsw             *fsnotify.Watcher
	dirs            []string
	includes        []string
	excludes        []string
	gitignore       []string
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

	w := &Watcher{
		fsw:          fsw,
		dirs:         opts.Dirs,
		includes:     opts.Includes,
		excludes:     excludes,
		outputBinary: opts.OutputBinary,
		workingDir:   wd,
		watched:      make(map[string]struct{}),
	}
	if len(w.dirs) == 0 {
		w.dirs = []string{"."}
	}
	if len(w.includes) == 0 {
		w.includes = []string{"*.go"}
	}
	if err := w.loadGitignore(); err != nil {
		_ = fsw.Close()
		return nil, err
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
		defer close(events)
		defer close(errs)
		defer w.fsw.Close()

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

func (w *Watcher) loadGitignore() error {
	path := filepath.Join(w.workingDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .gitignore: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "!") {
			continue
		}
		w.gitignore = append(w.gitignore, trim)
	}
	return nil
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
			if w.isDirExcluded(path) {
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

func (w *Watcher) tryAddDir(path string, errs chan<- error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	if w.isDirExcluded(path) {
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
	return w.matchesInclude(path)
}

func (w *Watcher) isDirExcluded(path string) bool {
	rel, err := filepath.Rel(w.workingDir, path)
	if err != nil {
		rel = path
	}
	clean := filepath.ToSlash(rel) + "/"
	for _, p := range w.excludes {
		if strings.HasSuffix(p, "/") {
			if strings.Contains(clean, trimLeadingDotSlash(filepath.ToSlash(p))) {
				return true
			}
		}
	}
	for _, p := range w.gitignore {
		if strings.HasSuffix(p, "/") && strings.Contains(clean, trimLeadingDotSlash(filepath.ToSlash(p))) {
			return true
		}
	}
	return false
}

func (w *Watcher) isExcluded(path string) bool {
	rel, _ := filepath.Rel(w.workingDir, path)
	rel = filepath.ToSlash(rel)
	base := filepath.Base(path)
	if rel == "" || rel == "." {
		rel = filepath.ToSlash(path)
	}
	if rel == filepath.ToSlash(w.outputBinary) || base == filepath.Base(w.outputBinary) {
		return true
	}
	for _, p := range append([]string{}, w.excludes...) {
		if patternMatch(rel, base, p) {
			return true
		}
	}
	for _, p := range w.gitignore {
		if patternMatch(rel, base, p) {
			return true
		}
	}
	return false
}

func (w *Watcher) matchesInclude(path string) bool {
	rel, _ := filepath.Rel(w.workingDir, path)
	rel = filepath.ToSlash(rel)
	base := filepath.Base(path)
	for _, p := range w.includes {
		if patternMatch(rel, base, p) {
			return true
		}
	}
	return false
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

func patternMatch(relPath, base, pattern string) bool {
	pat := trimLeadingDotSlash(filepath.ToSlash(strings.TrimSpace(pattern)))
	if pat == "" {
		return false
	}
	if strings.HasSuffix(pat, "/") {
		needle := pat
		if !strings.HasSuffix(needle, "/") {
			needle += "/"
		}
		rel := trimLeadingDotSlash(relPath)
		return strings.HasPrefix(rel, needle) || strings.Contains("/"+rel+"/", "/"+needle)
	}
	if ok, _ := filepath.Match(pat, base); ok {
		return true
	}
	if ok, _ := filepath.Match(pat, relPath); ok {
		return true
	}
	if strings.Contains(relPath, pat) {
		return true
	}
	return false
}

func trimLeadingDotSlash(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "./"), "/")
}
