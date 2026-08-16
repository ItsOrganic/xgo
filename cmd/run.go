package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ItsOrganic/xgo/internal/config"
	"github.com/ItsOrganic/xgo/internal/debouncer"
	"github.com/ItsOrganic/xgo/internal/logger"
	"github.com/ItsOrganic/xgo/internal/pathmatch"
	"github.com/ItsOrganic/xgo/internal/runner"
	"github.com/ItsOrganic/xgo/internal/watcher"

	"github.com/spf13/cobra"
)

func newRunCmd(opts *rootOptions) *cobra.Command {
	var (
		watchDirs []string
		excludes  []string
		includes  []string
		buildCmd  string
		runCmds   []string
		before    []string
		after     []string
		extraCmds []string
		verbose   bool
		deb       time.Duration
		timeout   time.Duration
	)

	cmd := &cobra.Command{
		Use:   "run [flags] [build-target] [-- app-args]",
		Short: "Run xgo hot reloader",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}

			inferredBuildTarget := ""
			appArgs := args
			if !cmd.Flags().Changed("build") && len(args) > 0 {
				target, ok, err := inferBuildTarget(wd, args[0])
				if err != nil {
					return err
				}
				if ok {
					inferredBuildTarget = target
					appArgs = args[1:]
				}
			}

			cfg, err := config.Load(opts.cfgFile)
			if err != nil {
				return err
			}
			ovr := config.Overrides{
				BuildCmd:      buildCmd,
				RunCmds:       runCmds,
				BeforeHooks:   before,
				AfterHooks:    after,
				ExtraCmds:     extraCmds,
				NoColor:       opts.noColor,
				Timestamps:    opts.timestamps,
				RunArgsAfterD: appArgs,
			}
			if cmd.Flags().Changed("watch") {
				ovr.WatchDirs = watchDirs
			}
			if cmd.Flags().Changed("include") {
				ovr.Includes = includes
			}
			if cmd.Flags().Changed("exclude") {
				ovr.Excludes = excludes
			}
			if cmd.Flags().Changed("debounce") {
				ovr.Debounce = &deb
			}
			if cmd.Flags().Changed("timeout") {
				ovr.Timeout = &timeout
			}
			if cmd.Flags().Changed("no-color") {
				ovr.NoColor = true
			}
			if cmd.Flags().Changed("timestamps") {
				ovr.Timestamps = true
			}
			config.MergeOverrides(&cfg, ovr)
			if inferredBuildTarget != "" {
				cfg.Build.Cmd = fmt.Sprintf("go build %s -o ./tmp/xgo-app %s", config.DefaultBuildFlags, shellQuote(inferredBuildTarget))
			}
			if err := config.Validate(cfg); err != nil {
				return err
			}

			if err := os.MkdirAll(filepath.Join(wd, "tmp"), 0o755); err != nil {
				return fmt.Errorf("create tmp directory: %w", err)
			}
			if err := ensureTmpGitignore(wd); err != nil {
				return err
			}

			log := logger.New(cfg.Log.Prefix, cfg.Log.Timestamps, cfg.Log.Color)
			if inferredBuildTarget != "" {
				log.Infof("inferred build target: %s", inferredBuildTarget)
			}
			gitignore, err := pathmatch.LoadGitignore(wd)
			if err != nil {
				return fmt.Errorf("read .gitignore: %w", err)
			}

			w, err := watcher.New(watcher.Options{
				Dirs:         cfg.Watch.Dirs,
				Includes:     cfg.Watch.Include,
				Excludes:     cfg.Watch.Exclude,
				Gitignore:    gitignore,
				Verbose:      verbose,
				OutputBinary: "tmp/xgo-app",
				WorkingDir:   wd,
			})
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			events, watchErrs := w.Start(ctx)
			if n := len(w.WatchedDirs()); n == 0 {
				log.Errorf("watching 0 dirs - no file changes will trigger a rebuild; check watch/exclude patterns and permissions")
			} else {
				log.Infof("watching %d dirs", n)
			}
			rawEvents := make(chan watcher.FileEvent, 256)
			go func() {
				defer close(rawEvents)
				for {
					select {
					case <-ctx.Done():
						return
					case evt, ok := <-events:
						if !ok {
							return
						}
						if verbose {
							log.Infof("event: %s %s", evt.EventType, evt.Path)
						}
						select {
						case rawEvents <- evt:
						case <-ctx.Done():
							return
						}
					}
				}
			}()

			db := debouncer.New(debouncer.Options{
				Delay:      cfg.Watch.Debounce,
				WatchDirs:  cfg.Watch.Dirs,
				Includes:   cfg.Watch.Include,
				Excludes:   append(cfg.Watch.Exclude, "vendor/", ".git/", "tmp/", "node_modules/", "*.pb.go", "*_mock.go"),
				Gitignore:  gitignore,
				WorkingDir: wd,
			})
			_ = db.PrimeFingerprint()
			signals := db.Start(ctx, rawEvents)

			runSpec := runner.CommandSpec{Name: "app", Cmd: cfg.Run.Cmd, Args: cfg.Run.Args, Env: cfg.Run.Env}
			extra := make([]runner.CommandSpec, 0, len(cfg.ExtraCmds))
			for i, c := range cfg.ExtraCmds {
				name := fmt.Sprintf("extra-%d", i+1)
				extra = append(extra, runner.CommandSpec{Name: name, Cmd: c.Cmd, Dir: c.Dir})
			}

			r := runner.New(runner.Config{
				BuildCmd:      cfg.Build.Cmd,
				BuildEnv:      cfg.Build.Env,
				BuildTimeout:  cfg.Build.Timeout,
				BeforeHooks:   cfg.Run.Before,
				AfterHooks:    cfg.Run.After,
				Main:          runSpec,
				Extra:         extra,
				WorkingDir:    wd,
				OutputBinary:  "tmp/xgo-app",
				Logger:        log,
				SignalTimeout: 5 * time.Second,
			})

			baseState := runtimeState{
				PID:           os.Getpid(),
				WatchedDirs:   w.WatchedDirs(),
				Include:       append([]string{}, cfg.Watch.Include...),
				Exclude:       append([]string{}, cfg.Watch.Exclude...),
				Debounce:      cfg.Watch.Debounce,
				FoundConfig:   cfg.FoundFile,
				ConfigPath:    cfg.Path,
				BuildCmd:      cfg.Build.Cmd,
				RunCmd:        cfg.Run.Cmd,
				ExtraCommands: collectExtra(extra),
				Hooks: map[string][]string{
					"before": cfg.Run.Before,
					"after":  cfg.Run.After,
				},
			}
			statePath := stateFilePath(wd)
			_ = writeState(statePath, baseState)

			go func() {
				ticker := time.NewTicker(1 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						_ = writeState(statePath, stateFromSnapshot(baseState, r.Snapshot()))
						return
					case <-ticker.C:
						baseState.WatchedDirs = w.WatchedDirs()
						_ = writeState(statePath, stateFromSnapshot(baseState, r.Snapshot()))
					}
				}
			}()

			go func() {
				for err := range watchErrs {
					log.Warnf("watcher: %v", err)
				}
			}()

			if err := r.Run(ctx, signals); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().DurationVarP(&deb, "debounce", "d", 50*time.Millisecond, "Debounce delay")
	cmd.Flags().StringSliceVarP(&watchDirs, "watch", "w", nil, "Additional dirs to watch")
	cmd.Flags().StringSliceVarP(&excludes, "exclude", "x", nil, "Patterns to exclude")
	cmd.Flags().StringSliceVarP(&includes, "include", "i", []string{"*.go"}, "Patterns to include")
	cmd.Flags().StringVarP(&buildCmd, "build", "b", "", "Build command override")
	cmd.Flags().StringSliceVarP(&runCmds, "cmd", "c", nil, "Run command override (repeatable, supports parallel)")
	cmd.Flags().StringSliceVar(&before, "before", nil, "Commands to run before build")
	cmd.Flags().StringSliceVar(&after, "after", nil, "Commands to run after restart")
	cmd.Flags().StringSliceVar(&extraCmds, "extra-cmd", nil, "Additional parallel commands")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Build timeout")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output (show fsnotify raw events)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func ensureTmpGitignore(wd string) error {
	path := filepath.Join(wd, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read .gitignore: %w", err)
	}
	lines := []string{}
	if len(data) > 0 {
		lines = strings.Split(string(data), "\n")
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "tmp/" || strings.TrimSpace(l) == "tmp" {
			return nil
		}
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, "tmp/")
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	return nil
}

func collectExtra(extra []runner.CommandSpec) []string {
	out := make([]string, 0, len(extra))
	for _, e := range extra {
		if strings.TrimSpace(e.Cmd) == "" {
			continue
		}
		out = append(out, e.Cmd)
	}
	sort.Strings(out)
	return out
}

func inferBuildTarget(wd, raw string) (string, bool, error) {
	p := raw
	if !filepath.IsAbs(p) {
		p = filepath.Join(wd, raw)
	}

	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("inspect build target %q: %w", raw, err)
	}

	if !info.IsDir() && !strings.HasSuffix(strings.ToLower(info.Name()), ".go") {
		return "", false, nil
	}

	rel, err := filepath.Rel(wd, p)
	if err != nil {
		return "", false, fmt.Errorf("resolve build target %q: %w", raw, err)
	}
	if rel == "" {
		rel = "."
	}
	return filepath.ToSlash(rel), true, nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
