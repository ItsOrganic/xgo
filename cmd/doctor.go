package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ItsOrganic/xgo/internal/config"
	"github.com/ItsOrganic/xgo/internal/pathmatch"
	"github.com/ItsOrganic/xgo/internal/watcher"

	"github.com/spf13/cobra"
)

// doctorCheck is one line of a `xgo doctor` report. ok+warn both false means
// a blocking failure; warn true (with ok false) means advisory-only.
type doctorCheck struct {
	name string
	ok   bool
	warn bool
	msg  string
}

// newDoctorCmd validates config + watch setup before committing to a
// long-running `xgo run` session. It exists specifically to catch, up front,
// the class of bug this project shipped early on: a watcher that silently
// registers zero directories, so file changes never trigger a rebuild and
// nothing about the running tool looks wrong until you notice a save didn't
// do anything.
func newDoctorCmd(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate xgo.yaml and your project's watch/build setup before running xgo run",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			out := cmd.OutOrStdout()
			var checks []doctorCheck

			cfg, err := config.Load(opts.cfgFile)
			if err != nil {
				checks = append(checks, doctorCheck{name: "config parses", msg: err.Error()})
				printDoctorReport(out, checks)
				return fmt.Errorf("doctor found blocking issues")
			}
			source := "defaults (no xgo.yaml found)"
			if cfg.FoundFile {
				source = cfg.Path
			}
			checks = append(checks, doctorCheck{name: "config parses", ok: true, msg: source})

			if err := config.Validate(cfg); err != nil {
				checks = append(checks, doctorCheck{name: "config is valid", msg: err.Error()})
			} else {
				checks = append(checks, doctorCheck{name: "config is valid", ok: true})
			}

			gitignore, _ := pathmatch.LoadGitignore(wd)
			w, err := watcher.New(watcher.Options{
				Dirs:         cfg.Watch.Dirs,
				Includes:     cfg.Watch.Include,
				Excludes:     cfg.Watch.Exclude,
				Gitignore:    gitignore,
				OutputBinary: config.DefaultOutputBinary,
				WorkingDir:   wd,
			})
			if err != nil {
				checks = append(checks, doctorCheck{name: "watch coverage", msg: err.Error()})
			} else {
				ctx, cancel := context.WithCancel(context.Background())
				_, _ = w.Start(ctx)
				n := len(w.WatchedDirs())
				cancel()
				if n == 0 {
					checks = append(checks, doctorCheck{name: "watch coverage", msg: "0 directories would be watched - check watch.dirs/watch.exclude and filesystem permissions; file changes would NOT trigger rebuilds"})
				} else {
					checks = append(checks, doctorCheck{name: "watch coverage", ok: true, msg: fmt.Sprintf("%d directories would be watched", n)})
				}
			}

			checks = append(checks, checkCommandLooksRunnable("build command", cfg.Build.Cmd, wd))
			checks = append(checks, checkCommandLooksRunnable("run command", cfg.Run.Cmd, wd))

			if printDoctorReport(out, checks) {
				return fmt.Errorf("doctor found blocking issues")
			}
			return nil
		},
	}
}

// checkCommandLooksRunnable is a best-effort, advisory-only check: it can
// only ever WARN, never FAIL, since a command that isn't resolvable yet
// (e.g. the compiled binary doesn't exist before the first build) or isn't
// on PATH (e.g. a shell builtin) is not necessarily broken.
func checkCommandLooksRunnable(name, cmdStr, wd string) doctorCheck {
	trim := strings.TrimSpace(cmdStr)
	if trim == "" {
		return doctorCheck{name: name, warn: true, msg: "not configured"}
	}
	fields := strings.Fields(trim)
	token := fields[0]

	if strings.ContainsAny(token, "/\\") {
		p := token
		if !filepath.IsAbs(p) {
			p = filepath.Join(wd, token)
		}
		if _, err := os.Stat(p); err != nil {
			return doctorCheck{name: name, warn: true, msg: fmt.Sprintf("%q not found yet (expected if it's the build output, before the first successful build)", token)}
		}
		return doctorCheck{name: name, ok: true, msg: trim}
	}

	if _, err := exec.LookPath(token); err != nil {
		return doctorCheck{name: name, warn: true, msg: fmt.Sprintf("%q not found on PATH (could still be a shell builtin)", token)}
	}
	return doctorCheck{name: name, ok: true, msg: trim}
}

func printDoctorReport(out io.Writer, checks []doctorCheck) (blocking bool) {
	for _, c := range checks {
		status := "PASS"
		switch {
		case c.ok:
			status = "PASS"
		case c.warn:
			status = "WARN"
		default:
			status = "FAIL"
			blocking = true
		}
		if c.msg != "" {
			fmt.Fprintf(out, "[%s] %s: %s\n", status, c.name, c.msg)
		} else {
			fmt.Fprintf(out, "[%s] %s\n", status, c.name)
		}
	}
	if blocking {
		fmt.Fprintln(out, "\nBlocking issues found - fix the FAIL lines above before running `xgo run`.")
	}
	return blocking
}
