package debouncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xgo/internal/watcher"
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
	WorkingDir string
}

// Debouncer coalesces watcher events and emits build signals.
type Debouncer struct {
	delay           time.Duration
	watchDirs       []string
	includes        []string
	excludes        []string
	workingDir      string
	lastFingerprint string
}

// New creates a debouncer.
func New(opts Options) *Debouncer {
	if opts.Delay <= 0 {
		opts.Delay = 50 * time.Millisecond
	}
	if len(opts.WatchDirs) == 0 {
		opts.WatchDirs = []string{"."}
	}
	if len(opts.Includes) == 0 {
		opts.Includes = []string{"*.go"}
	}
	return &Debouncer{
		delay:      opts.Delay,
		watchDirs:  opts.WatchDirs,
		includes:   opts.Includes,
		excludes:   opts.Excludes,
		workingDir: opts.WorkingDir,
	}
}

// Start starts the coalescing debouncer loop.
func (d *Debouncer) Start(ctx context.Context, in <-chan watcher.FileEvent) <-chan BuildSignal {
	out := make(chan BuildSignal, 1)
	go func() {
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
				fingerprint, err := d.fingerprint()
				if err == nil && fingerprint != "" && fingerprint == d.lastFingerprint {
					queue = queue[:0]
					timer = nil
					tickCh = nil
					continue
				}
				if err == nil {
					d.lastFingerprint = fingerprint
				}
				signal := BuildSignal{Time: time.Now(), Fingerprint: fingerprint, Events: append([]watcher.FileEvent(nil), queue...)}
				select {
				case out <- signal:
				default:
				}
				queue = queue[:0]
				timer = nil
				tickCh = nil
			}
		}
	}()
	return out
}

// PrimeFingerprint sets initial fingerprint.
func (d *Debouncer) PrimeFingerprint() error {
	fp, err := d.fingerprint()
	if err != nil {
		return err
	}
	d.lastFingerprint = fp
	return nil
}

func (d *Debouncer) fingerprint() (string, error) {
	h := sha256.New()
	files := make([]string, 0, 256)
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
				if d.isExcluded(path, true) {
					return filepath.SkipDir
				}
				return nil
			}
			if d.isExcluded(path, false) || !d.isIncluded(path) {
				return nil
			}
			files = append(files, path)
			return nil
		})
	}
	sort.Strings(files)
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		line := fmt.Sprintf("%s|%d|%d\n", f, info.Size(), info.ModTime().UnixNano())
		_, _ = h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (d *Debouncer) isIncluded(path string) bool {
	base := filepath.Base(path)
	rel := toRel(d.workingDir, path)
	for _, p := range d.includes {
		if matchPattern(rel, base, p) {
			return true
		}
	}
	return false
}

func (d *Debouncer) isExcluded(path string, isDir bool) bool {
	base := filepath.Base(path)
	rel := toRel(d.workingDir, path)
	for _, p := range d.excludes {
		pat := strings.TrimSpace(p)
		if pat == "" {
			continue
		}
		if isDir && strings.HasSuffix(pat, "/") && strings.Contains(rel+"/", strings.TrimSuffix(pat, "/")+"/") {
			return true
		}
		if matchPattern(rel, base, pat) {
			return true
		}
	}
	return false
}

func toRel(wd, path string) string {
	rel, err := filepath.Rel(wd, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func matchPattern(rel, base, pat string) bool {
	pat = filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(pat), "./"), "/"))
	if pat == "" {
		return false
	}
	if strings.HasSuffix(pat, "/") {
		return strings.Contains(rel+"/", strings.TrimSuffix(pat, "/")+"/")
	}
	if ok, _ := filepath.Match(pat, base); ok {
		return true
	}
	if ok, _ := filepath.Match(pat, rel); ok {
		return true
	}
	return strings.Contains(rel, pat)
}
