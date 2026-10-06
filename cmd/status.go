package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ItsOrganic/xgo/internal/runner"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show xgo watch/runtime status",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			path := stateFilePath(wd)
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "No active xgo status found. Run `xgo run` first.")
					return nil
				}
				return fmt.Errorf("read status file: %w", err)
			}

			var st runtimeState
			if err := yaml.Unmarshal(data, &st); err != nil {
				return fmt.Errorf("parse status file: %w", err)
			}

			out := cmd.OutOrStdout()
			switch {
			case st.PID <= 0:
				fmt.Fprintln(out, "Status: unknown (no pid recorded - this state file predates the liveness check)")
			case runner.IsAlive(st.PID):
				fmt.Fprintf(out, "Status: RUNNING (pid=%d)\n", st.PID)
			default:
				fmt.Fprintf(out, "Status: NOT RUNNING (stale - last updated %s)\n", st.UpdatedAt.Format("2006-01-02 15:04:05"))
			}
			fmt.Fprintf(out, "Updated: %s\n", st.UpdatedAt.Format("2006-01-02 15:04:05"))
			fmt.Fprintf(out, "Config: %s (found=%t)\n", st.ConfigPath, st.FoundConfig)
			fmt.Fprintf(out, "Debounce: %s\n", st.Debounce)
			fmt.Fprintf(out, "Build cmd: %s\n", st.BuildCmd)
			fmt.Fprintf(out, "Run cmd: %s\n", st.RunCmd)
			if len(st.ExtraCommands) > 0 {
				fmt.Fprintf(out, "Extra cmds: %s\n", strings.Join(st.ExtraCommands, ", "))
			}

			fmt.Fprintln(out, "Watched directories:")
			for _, d := range st.WatchedDirs {
				fmt.Fprintf(out, "  - %s\n", d)
			}
			fmt.Fprintf(out, "Include patterns: %s\n", strings.Join(st.Include, ", "))
			fmt.Fprintf(out, "Exclude patterns: %s\n", strings.Join(st.Exclude, ", "))

			fmt.Fprintln(out, "Running child processes:")
			names := make([]string, 0, len(st.PIDs))
			for name := range st.PIDs {
				names = append(names, name)
			}
			sort.Strings(names)
			if len(names) == 0 {
				fmt.Fprintln(out, "  (none)")
			} else {
				for _, n := range names {
					fmt.Fprintf(out, "  - %s: pid=%d\n", n, st.PIDs[n])
				}
			}

			fmt.Fprintf(out, "Restart count: %d\n", st.RestartCount)
			if !st.LastRestart.IsZero() {
				fmt.Fprintf(out, "Last restart: %s\n", st.LastRestart.Format("2006-01-02 15:04:05"))
			}
			fmt.Fprintf(out, "Last build duration: %s\n", st.LastBuild)
			fmt.Fprintf(out, "Last build failed: %t\n", st.LastBuildError)
			return nil
		},
	}
}
