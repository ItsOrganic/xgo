package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_NoFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "xgo.yaml"))
	if err != nil {
		t.Fatalf("Load with no file present must not error: %v", err)
	}
	if cfg.FoundFile {
		t.Fatalf("FoundFile should be false when no config file exists")
	}
	if cfg.Build.Cmd != DefaultBuildCmd {
		t.Errorf("unexpected default build cmd: %q", cfg.Build.Cmd)
	}
	// The stripping flags are a deliberate, measured default (see
	// DefaultBuildFlags) - assert them explicitly so silently dropping them
	// shows up as a test failure rather than as a 20% build-time regression.
	if !strings.Contains(cfg.Build.Cmd, `-ldflags="-s -w"`) {
		t.Errorf("default build cmd should ship the stripping flags, got %q", cfg.Build.Cmd)
	}
	if cfg.Watch.Debounce != 50*time.Millisecond {
		t.Errorf("unexpected default debounce: %v", cfg.Watch.Debounce)
	}
}

func TestLoad_UnknownField_IsRejected(t *testing.T) {
	// Regression test: xgo.yaml used to silently ignore unrecognized keys
	// (e.g. a typo like "debouce" instead of "debounce"), so a config
	// mistake produced no error and quietly fell back to the default.
	dir := t.TempDir()
	path := filepath.Join(dir, "xgo.yaml")
	if err := os.WriteFile(path, []byte("watch:\n  debouce: 100ms\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for an unrecognized config key, got nil")
	}
}

func TestLoad_ValidFile_OverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "xgo.yaml")
	yaml := "watch:\n  debounce: 250ms\nbuild:\n  cmd: \"echo hi\"\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.FoundFile {
		t.Error("FoundFile should be true")
	}
	if cfg.Watch.Debounce != 250*time.Millisecond {
		t.Errorf("debounce override not applied: got %v", cfg.Watch.Debounce)
	}
	if cfg.Build.Cmd != "echo hi" {
		t.Errorf("build.cmd override not applied: got %q", cfg.Build.Cmd)
	}
	// Untouched fields should still carry their defaults.
	if cfg.Run.Cmd != DefaultRunCmd {
		t.Errorf("unrelated default was clobbered: run.cmd = %q", cfg.Run.Cmd)
	}
}

func TestMergeOverrides_CLIWinsOverFile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Build.Cmd = "from file"

	deb := 999 * time.Millisecond
	MergeOverrides(&cfg, Overrides{
		BuildCmd: "from cli",
		Debounce: &deb,
	})

	if cfg.Build.Cmd != "from cli" {
		t.Errorf("CLI override should win: got %q", cfg.Build.Cmd)
	}
	if cfg.Watch.Debounce != deb {
		t.Errorf("CLI debounce override should win: got %v", cfg.Watch.Debounce)
	}
}

func TestMergeOverrides_UnsetCLIFieldsDontClobberFile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Build.Cmd = "from file"
	MergeOverrides(&cfg, Overrides{}) // nothing set via CLI
	if cfg.Build.Cmd != "from file" {
		t.Errorf("an empty override must not clobber a file-provided value: got %q", cfg.Build.Cmd)
	}
}

func TestMergeOverrides_RunCmdsSplitsMainAndExtra(t *testing.T) {
	cfg := DefaultConfig()
	MergeOverrides(&cfg, Overrides{RunCmds: []string{"./main", "npm run watch"}})
	if cfg.Run.Cmd != "./main" {
		t.Errorf("first -c should become run.cmd: got %q", cfg.Run.Cmd)
	}
	if len(cfg.ExtraCmds) != 1 || cfg.ExtraCmds[0].Cmd != "npm run watch" {
		t.Errorf("subsequent -c flags should become extra_cmds: got %+v", cfg.ExtraCmds)
	}
}

func TestValidate_AcceptsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if err := Validate(cfg); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestValidate_RejectsNegativeDurations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Watch.Debounce = -10 * time.Millisecond
	if err := Validate(cfg); err == nil {
		t.Fatal("expected an error for negative debounce")
	}

	cfg = DefaultConfig()
	cfg.Build.Timeout = -1 * time.Second
	if err := Validate(cfg); err == nil {
		t.Fatal("expected an error for negative build timeout")
	}
}

func TestValidate_RejectsBlankListEntries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Watch.Dirs = []string{".", "  "}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected an error for a blank watch.dirs entry")
	}
}

func TestValidate_RejectsBlankExtraCmd(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExtraCmds = []ExtraCmd{{Cmd: "   "}}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected an error for a blank extra_cmds entry")
	}
}

func TestApplyDefaults_ZeroMeansUnsetNegativeIsPreserved(t *testing.T) {
	cfg := Config{}
	applyDefaults(&cfg)
	if cfg.Watch.Debounce != 50*time.Millisecond {
		t.Errorf("zero debounce should default to 50ms, got %v", cfg.Watch.Debounce)
	}

	cfg = Config{Watch: WatchConfig{Debounce: -5 * time.Millisecond}}
	applyDefaults(&cfg)
	if cfg.Watch.Debounce != -5*time.Millisecond {
		t.Errorf("a negative debounce must be left alone by applyDefaults so Validate can catch it, got %v", cfg.Watch.Debounce)
	}
}

func TestDefaultBinaryPaths_PerOS(t *testing.T) {
	cases := []struct{ goos, output, run string }{
		{"linux", "tmp/xgo-app", "./tmp/xgo-app"},
		{"darwin", "tmp/xgo-app", "./tmp/xgo-app"},
		{"windows", "tmp/xgo-app.exe", `tmp\xgo-app.exe`},
	}
	for _, c := range cases {
		if got := outputBinaryFor(c.goos); got != c.output {
			t.Errorf("outputBinaryFor(%q) = %q, want %q", c.goos, got, c.output)
		}
		if got := runCmdFor(c.goos); got != c.run {
			t.Errorf("runCmdFor(%q) = %q, want %q", c.goos, got, c.run)
		}
	}
}
