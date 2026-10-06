package cmd

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/ItsOrganic/xgo/internal/config"
	"github.com/spf13/cobra"
)

func TestRootCmd_IsNamedXgo(t *testing.T) {
	root := NewRootCmd()
	if root.Use != "xgo" {
		t.Fatalf("root.Use = %q, want %q", root.Use, "xgo")
	}
}

// The --config default is a string literal the compiler cannot check.
// If the rename misses it, users get "xgo.yaml not found" from a tool
// that no longer ships anything by that name.
func TestRootCmd_DefaultConfigIsXgoYAML(t *testing.T) {
	root := NewRootCmd()
	f := root.PersistentFlags().Lookup("config")
	if f == nil {
		t.Fatal("no --config flag registered")
	}
	if f.DefValue != "xgo.yaml" {
		t.Fatalf("--config default = %q, want %q", f.DefValue, "xgo.yaml")
	}
}

// Same hazard: the output binary path lives inside a build-command string.
func TestDefaultBuildCmd_TargetsXgoApp(t *testing.T) {
	if !strings.Contains(config.DefaultBuildCmd, "./tmp/xgo-app") {
		t.Fatalf("DefaultBuildCmd = %q, want it to target ./tmp/xgo-app", config.DefaultBuildCmd)
	}
	if strings.Contains(config.DefaultBuildCmd, "whack") {
		t.Fatalf("DefaultBuildCmd still mentions whack: %q", config.DefaultBuildCmd)
	}
}

func TestNewRootCmd_UsesPackageVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "1.2.3"

	root := NewRootCmd()
	if root.Version != "1.2.3" {
		t.Fatalf("root.Version = %q, want %q", root.Version, "1.2.3")
	}
}

func TestRootCmd_VersionFlagPrintsVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "9.9.9"

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	if !strings.Contains(buf.String(), "9.9.9") {
		t.Fatalf("--version output = %q, want it to contain %q", buf.String(), "9.9.9")
	}
}

// findSubCmd returns the immediate child of root named name, or nil.
func findSubCmd(root *cobra.Command, name string) *cobra.Command {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// Task 12, test 1: bare `xgo` (no subcommand) must reach run's behaviour,
// not cobra's default help-on-unrunnable-command fallback. We don't execute
// the RunE (it starts a long-running watcher and attempts a real build) -
// instead we assert root.RunE is wired to the exact same function as the
// registered `run` subcommand's RunE, which is what makes bare `xgo`
// behave as `xgo run`.
func TestRootCmd_BareInvocationReachesRun(t *testing.T) {
	root := NewRootCmd()

	runSub := findSubCmd(root, "run")
	if runSub == nil {
		t.Fatal("no \"run\" subcommand registered")
	}
	if root.RunE == nil {
		t.Fatal("root.RunE is nil; bare `xgo` would fall back to cobra's default help output instead of running")
	}

	rootPtr := reflect.ValueOf(root.RunE).Pointer()
	runPtr := reflect.ValueOf(runSub.RunE).Pointer()
	if rootPtr != runPtr {
		t.Fatalf("root.RunE (%v) is not run's RunE (%v); bare `xgo` does not behave as `xgo run`", rootPtr, runPtr)
	}
}

// Task 12, test 2: `xgo init` must still resolve to the init subcommand -
// subcommand resolution wins over root's new default RunE.
func TestRootCmd_InitStillResolvesToInit(t *testing.T) {
	root := NewRootCmd()

	target, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatalf("root.Find([\"init\"]): %v", err)
	}
	if target.Name() != "init" {
		t.Fatalf("`xgo init` resolved to %q, want %q", target.Name(), "init")
	}
}

// Task 12, test 3: `xgo doctor` and `xgo status` must still resolve to
// their own commands, not root's default run behaviour.
func TestRootCmd_DoctorAndStatusStillResolveToOwnCommands(t *testing.T) {
	root := NewRootCmd()

	for _, name := range []string{"doctor", "status"} {
		target, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("root.Find([%q]): %v", name, err)
		}
		if target.Name() != name {
			t.Fatalf("`xgo %s` resolved to %q, want %q", name, target.Name(), name)
		}
	}
}

// Task 12, test 4: `xgo --version` must still print the version and must
// NOT start the watcher. Cobra's built-in version handling returns before
// RunE is ever invoked (see execute() in spf13/cobra), so if this prints the
// version at all, the watcher necessarily never started - there is no
// separate "did it start the watcher" signal to check.
func TestRootCmd_VersionFlagDoesNotStartWatcher(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "7.7.7"

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	if !strings.Contains(buf.String(), "7.7.7") {
		t.Fatalf("--version output = %q, want it to contain %q", buf.String(), "7.7.7")
	}
}

// Task 12, test 5: the design's flag-sharing trick. A run flag parsed on
// root must be visible to run's own Changed() check - this is the exact
// subtlety the plan calls out: run.go reads overrides via
// cmd.Flags().Changed("build") etc, so whichever command actually executes
// must own those flags. AddFlagSet shares *pflag.Flag pointers, so parsing
// on root's flag set must flip Changed() on run's flag set too.
func TestRootCmd_RunFlagParsedOnRootVisibleToRunChanged(t *testing.T) {
	root := NewRootCmd()

	runSub := findSubCmd(root, "run")
	if runSub == nil {
		t.Fatal("no \"run\" subcommand registered")
	}

	if err := root.Flags().Parse([]string{"--build", "go build ./..."}); err != nil {
		t.Fatalf("parse --build on root.Flags(): %v", err)
	}
	if !runSub.Flags().Changed("build") {
		t.Fatal("run's Flags().Changed(\"build\") is false after parsing --build on root; the flag set is not shared, so run.go's Changed() overrides would silently no-op")
	}
}

// Task 12, test 6: `xgo run` must still work explicitly - its own flags
// still parse and set Changed() on its own flag set when invoked by name.
func TestRootCmd_ExplicitRunStillWorks(t *testing.T) {
	root := NewRootCmd()

	target, _, err := root.Find([]string{"run"})
	if err != nil {
		t.Fatalf("root.Find([\"run\"]): %v", err)
	}
	if target.Name() != "run" {
		t.Fatalf("`xgo run` resolved to %q, want %q", target.Name(), "run")
	}
	if err := target.ParseFlags([]string{"--build", "echo hi"}); err != nil {
		t.Fatalf("parse --build on explicit run command: %v", err)
	}
	if !target.Flags().Changed("build") {
		t.Fatal("explicit `xgo run --build ...` did not set Changed(\"build\") on run's own flags")
	}
}
