package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ItsOrganic/whack/internal/config"
)

func TestRootCmd_IsNamedWhack(t *testing.T) {
	root := NewRootCmd()
	if root.Use != "whack" {
		t.Fatalf("root.Use = %q, want %q", root.Use, "whack")
	}
}

// The --config default is a string literal the compiler cannot check.
// If the rename misses it, users get "xgo.yaml not found" from a tool
// that no longer ships anything by that name.
func TestRootCmd_DefaultConfigIsWhackYAML(t *testing.T) {
	root := NewRootCmd()
	f := root.PersistentFlags().Lookup("config")
	if f == nil {
		t.Fatal("no --config flag registered")
	}
	if f.DefValue != "whack.yaml" {
		t.Fatalf("--config default = %q, want %q", f.DefValue, "whack.yaml")
	}
}

// Same hazard: the output binary path lives inside a build-command string.
func TestDefaultBuildCmd_TargetsWhackApp(t *testing.T) {
	if !strings.Contains(config.DefaultBuildCmd, "./tmp/whack-app") {
		t.Fatalf("DefaultBuildCmd = %q, want it to target ./tmp/whack-app", config.DefaultBuildCmd)
	}
	if strings.Contains(config.DefaultBuildCmd, "xgo") {
		t.Fatalf("DefaultBuildCmd still mentions xgo: %q", config.DefaultBuildCmd)
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
