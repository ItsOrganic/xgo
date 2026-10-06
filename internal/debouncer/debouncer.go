package debouncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ItsOrganic/xgo/internal/pathmatch"
	"github.com/ItsOrganic/xgo/internal/watcher"
)

// BuildSignal indicates a confirmed source change that should trigger build/restart.
type BuildSignal struct {
	Time        time.Time
	Fingerprint string
	Events      []watcher.FileEvent
}

// Options configures debouncer behavior.
type Options struct {
	Delay      time.Duration
	WatchDirs  []string
	Includes   []string
	Excludes   []string
	Gitignore  []string
	WorkingDir string
}

type stamp struct {
	size  int64
	mtime int64
}

// Debouncer coalesces watcher events and emits build signals.
type Debouncer struct {
	delay           time.Duration
	watchDirs       []string
	workingDir      string
	matcher         *pathmatch.Matcher
	lastFingerprint string

	// files is an incrementally-maintained index of every matched file's
	// (size, mtime). It's seeded once by a full walk in PrimeFingerprint,
	// then updated only for paths that a FileEvent actually reports as
	// changed - avoiding a full filesystem re-walk + re-stat of every
	// watched file on every single debounce tick, which is what the
	// original implementation did.
	files map[string]stamp
}

// New creates a debouncer.
func New(opts Options) *Debouncer {
	if opts.Delay <= 0 {
		opts.Delay = 50 * time.Millisecond
	}
	watchDirs := opts.WatchDirs
	if len(watchDirs) == 0 {
		watchDirs = []string{"."}
	}
	includes := opts.Includes
	if len(includes) == 0 {
		includes = []string{"*.go"}
	}
	return &Debouncer{
		delay:      opts.Delay,
		watchDirs:  watchDirs,
		workingDir: opts.WorkingDir,
		matcher:    pathmatch.New(opts.WorkingDir, includes, opts.Excludes, opts.Gitignore),
		files:      make(map[string]stamp),
	}
}

// Start starts the coalescing debouncer loop.
func (d *Debouncer) Start(ctx context.Context, in <-chan watcher.FileEvent) <-chan BuildSignal {
	out := make(chan BuildSignal, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[xgo] recovered from panic in debouncer: %v\n", r)
			}
		}()
		defer close(out)
		var (
			timer  *time.Timer
			queue  []watcher.FileEvent
			tickCh <-chan time.Time
		)

		for {
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case evt, ok := <-in:
				if !ok {
					if timer != nil {
						timer.Stop()
					}
					return
				}
				queue = append(queue, evt)
				if timer == nil {
					timer = time.NewTimer(d.delay)
					tickCh = timer.C
					continue
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(d.delay)
			case <-tickCh:
				for _, evt := range queue {
					d.applyEvent(evt)
				}
				fingerprint := d.computeFingerprint()
				if fingerprint == d.lastFingerprint {
					queue = queue[:0]
					timer = nil
					tickCh = nil
					continue
				}
				d.lastFingerprint = fingerprint
				signal := BuildSignal{Time: time.Now(), Fingerprint: fingerprint, Events: append([]watcher.FileEvent(nil), queue...)}
				select {
				case out <- signal:
				default:
					// Buffer full: drop the stale pending signal and replace it
					// with this newer one, so the latest edit is never lost.
					select {
					case <-out:
					default:
					}
					select {
					case out <- signal:
					default:
					}
				}
				queue = queue[:0]
				timer = nil
				tickCh = nil
			}
		}
	}()
	return out
}

// PrimeFingerprint performs the one-time full walk that seeds the in-memory
// file index, and computes the initial fingerprint so the first real edit
// has something to diff against.
func (d *Debouncer) PrimeFingerprint() error {
	files := make(map[string]stamp, len(d.files))
	for _, dir := range d.watchDirs {
		root := dir
		if !filepath.IsAbs(root) {
			root = filepath.Join(d.workingDir, root)
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if d.matcher.ExcludedDir(path) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.matcher.MatchFile(path) {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			files[path] = stamp{size: info.Size(), mtime: info.ModTime().UnixNano()}
			return nil
		})
	}
	d.files = files
	d.lastFingerprint = d.computeFingerprint()
	return nil
}

// applyEvent updates the in-memory file index for a single reported change,
// stat-ing only that one path rather than re-walking the whole tree.
func (d *Debouncer) applyEvent(evt watcher.FileEvent) {
	path := evt.Path
	if evt.EventType == "remove" || evt.EventType == "rename" {
		delete(d.files, path)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		// Gone by the time we got to it (e.g. a rename we saw as "create"
		// racing a subsequent delete) - treat like a removal.
		delete(d.files, path)
		return
	}
	if info.IsDir() {
		return
	}
	if !d.matcher.MatchFile(path) {
		delete(d.files, path)
		return
	}
	d.files[path] = stamp{size: info.Size(), mtime: info.ModTime().UnixNano()}
}

// computeFingerprint hashes the current in-memory file index. This is pure
// CPU/memory work (sorting and hashing small strings) with zero syscalls -
// cheap even for tens of thousands of entries - unlike the walk+stat pass
// that seeds it, which only ever runs once in PrimeFingerprint.
func (d *Debouncer) computeFingerprint() string {
	h := sha256.New()
	paths := make([]string, 0, len(d.files))
	for p := range d.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		s := d.files[p]
		fmt.Fprintf(h, "%s|%d|%d\n", p, s.size, s.mtime)
	}
	return hex.EncodeToString(h.Sum(nil))
}
