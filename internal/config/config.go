package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents xgo configuration loaded from xgo.yaml and CLI flags.
type Config struct {
	Watch     WatchConfig `yaml:"watch"`
	Build     BuildConfig `yaml:"build"`
	Run       RunConfig   `yaml:"run"`
	ExtraCmds []ExtraCmd  `yaml:"extra_cmds"`
	Log       LogConfig   `yaml:"log"`
	ConfigDir string      `yaml:"-"`
	FoundFile bool        `yaml:"-"`
	Path      string      `yaml:"-"`
}

// WatchConfig configures file watching.
type WatchConfig struct {
	Dirs     []string      `yaml:"dirs"`
	Include  []string      `yaml:"include"`
	Exclude  []string      `yaml:"exclude"`
	Debounce time.Duration `yaml:"debounce"`
}

// BuildConfig configures the build command.
type BuildConfig struct {
	Cmd     string        `yaml:"cmd"`
	Env     []string      `yaml:"env"`
	Timeout time.Duration `yaml:"timeout"`
}

// RunConfig configures runtime command and hooks.
type RunConfig struct {
	Cmd    string   `yaml:"cmd"`
	Args   []string `yaml:"args"`
	Env    []string `yaml:"env"`
	Before []string `yaml:"before"`
	After  []string `yaml:"after"`
}

// ExtraCmd configures a parallel extra command.
type ExtraCmd struct {
	Cmd string `yaml:"cmd"`
	Dir string `yaml:"dir"`
}

// LogConfig configures logger output.
type LogConfig struct {
	Timestamps bool   `yaml:"timestamps"`
	Color      bool   `yaml:"color"`
	Prefix     string `yaml:"prefix"`
}

// Overrides contains CLI-provided values.
type Overrides struct {
	WatchDirs     []string
	Includes      []string
	Excludes      []string
	Debounce      *time.Duration
	BuildCmd      string
	RunCmds       []string
	BeforeHooks   []string
	AfterHooks    []string
	ExtraCmds     []string
	NoColor       bool
	Timestamps    bool
	Timeout       *time.Duration
	RunArgsAfterD []string
}

// DefaultBuildFlags are the linker flags xgo's own default build command
// ships with. -s and -w drop the symbol table and DWARF debug info, neither
// of which the hot-reload loop ever reads. Linking is the single most
// expensive phase of a rebuild (~350-400ms of a ~640ms cycle), and skipping
// that output cuts it measurably: 624ms -> 500ms per build and 7.56MB ->
// 5.15MB of produced binary on benchmark/testapps/minimal, which also
// lowers the running app's own resident memory.
//
// This applies only to the command xgo generates for you. An explicit
// build.cmd in xgo.yaml is used verbatim, so anyone who needs symbols (to
// attach delve, say) just drops the flags from their own config.
const DefaultBuildFlags = `-ldflags="-s -w"`

// DefaultOutputBinary is where xgo's generated build command writes the app,
// relative to the project root. Windows needs the .exe suffix: `go build -o`
// writes exactly the name it is given, and cmd.exe won't run a file without
// an executable extension.
var DefaultOutputBinary = outputBinaryFor(runtime.GOOS)

// DefaultRunCmd runs DefaultOutputBinary. On Windows it is a backslash path,
// since cmd.exe parses the "/tmp" in "./tmp/xgo-app" as a command switch.
var DefaultRunCmd = runCmdFor(runtime.GOOS)

// DefaultBuildCmd is the build command used when the config doesn't set one.
var DefaultBuildCmd = `go build ` + DefaultBuildFlags + ` -o ` + DefaultRunCmd + ` .`

func outputBinaryFor(goos string) string {
	if goos == "windows" {
		return "tmp/xgo-app.exe"
	}
	return "tmp/xgo-app"
}

func runCmdFor(goos string) string {
	if goos == "windows" {
		return `tmp\xgo-app.exe`
	}
	return "./" + outputBinaryFor(goos)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Watch: WatchConfig{
			Dirs:     []string{"."},
			Include:  []string{"*.go"},
			Exclude:  []string{},
			Debounce: 50 * time.Millisecond,
		},
		Build: BuildConfig{
			Cmd:     DefaultBuildCmd,
			Timeout: 30 * time.Second,
		},
		Run: RunConfig{
			Cmd: DefaultRunCmd,
		},
		Log: LogConfig{
			Timestamps: false,
			Color:      true,
			Prefix:     "[xgo]",
		},
	}
}

// Load loads configuration from file if present and merges defaults.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		path = "xgo.yaml"
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg.Path = absPath
	cfg.ConfigDir = filepath.Dir(absPath)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	cfg.FoundFile = true

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w (check for typo'd keys - unknown fields are rejected rather than silently ignored)", path, err)
	}

	applyDefaults(&cfg)
	return cfg, nil
}

// MergeOverrides applies CLI overrides over config values.
func MergeOverrides(cfg *Config, o Overrides) {
	if len(o.WatchDirs) > 0 {
		cfg.Watch.Dirs = uniqueStrings(append(cfg.Watch.Dirs, o.WatchDirs...))
	}
	if len(o.Includes) > 0 {
		cfg.Watch.Include = o.Includes
	}
	if len(o.Excludes) > 0 {
		cfg.Watch.Exclude = uniqueStrings(append(cfg.Watch.Exclude, o.Excludes...))
	}
	if o.Debounce != nil {
		cfg.Watch.Debounce = *o.Debounce
	}
	if o.BuildCmd != "" {
		cfg.Build.Cmd = o.BuildCmd
	}
	if o.Timeout != nil {
		cfg.Build.Timeout = *o.Timeout
	}
	if len(o.RunCmds) > 0 {
		cfg.Run.Cmd = o.RunCmds[0]
		if len(o.RunCmds) > 1 {
			cfg.ExtraCmds = append(cfg.ExtraCmds, extraCmdsFromRaw(o.RunCmds[1:])...)
		}
	}
	if len(o.ExtraCmds) > 0 {
		cfg.ExtraCmds = append(cfg.ExtraCmds, extraCmdsFromRaw(o.ExtraCmds)...)
	}
	if len(o.BeforeHooks) > 0 {
		cfg.Run.Before = o.BeforeHooks
	}
	if len(o.AfterHooks) > 0 {
		cfg.Run.After = o.AfterHooks
	}
	if o.NoColor {
		cfg.Log.Color = false
	}
	if o.Timestamps {
		cfg.Log.Timestamps = true
	}
	if len(o.RunArgsAfterD) > 0 {
		cfg.Run.Args = append(cfg.Run.Args, o.RunArgsAfterD...)
	}
	applyDefaults(cfg)
}

func extraCmdsFromRaw(cmds []string) []ExtraCmd {
	out := make([]ExtraCmd, 0, len(cmds))
	for _, c := range cmds {
		trim := strings.TrimSpace(c)
		if trim == "" {
			continue
		}
		out = append(out, ExtraCmd{Cmd: trim})
	}
	return out
}

func applyDefaults(cfg *Config) {
	if len(cfg.Watch.Dirs) == 0 {
		cfg.Watch.Dirs = []string{"."}
	}
	if len(cfg.Watch.Include) == 0 {
		cfg.Watch.Include = []string{"*.go"}
	}
	// Only exact zero means "unset" - a negative value is a typo, not a
	// request for the default, and is caught by Validate instead of being
	// silently coerced here.
	if cfg.Watch.Debounce == 0 {
		cfg.Watch.Debounce = 50 * time.Millisecond
	}
	if strings.TrimSpace(cfg.Build.Cmd) == "" {
		cfg.Build.Cmd = DefaultBuildCmd
	}
	if cfg.Build.Timeout == 0 {
		cfg.Build.Timeout = 30 * time.Second
	}
	if strings.TrimSpace(cfg.Run.Cmd) == "" {
		cfg.Run.Cmd = DefaultRunCmd
	}
	if strings.TrimSpace(cfg.Log.Prefix) == "" {
		cfg.Log.Prefix = "[xgo]"
	}
}

// Validate performs cheap, working-directory-independent structural checks
// on a fully-merged config. It runs after applyDefaults, so it deliberately
// does NOT re-check anything applyDefaults already backfills (an empty
// watch.dirs, for instance, is a legitimate "use the default" - not an
// error). It exists to catch the things a default can't paper over: a
// negative duration is a typo, not "unset"; a blank entry in a list is not
// "not configured".
func Validate(cfg Config) error {
	var problems []string

	if cfg.Watch.Debounce < 0 {
		problems = append(problems, fmt.Sprintf("watch.debounce must not be negative (got %s)", cfg.Watch.Debounce))
	}
	if cfg.Build.Timeout < 0 {
		problems = append(problems, fmt.Sprintf("build.timeout must not be negative (got %s)", cfg.Build.Timeout))
	}
	if hasBlankEntry(cfg.Watch.Dirs) {
		problems = append(problems, "watch.dirs contains a blank entry")
	}
	if hasBlankEntry(cfg.Watch.Include) {
		problems = append(problems, "watch.include contains a blank entry")
	}
	for i, c := range cfg.ExtraCmds {
		if strings.TrimSpace(c.Cmd) == "" {
			problems = append(problems, fmt.Sprintf("extra_cmds[%d].cmd must not be blank", i))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func hasBlankEntry(list []string) bool {
	for _, s := range list {
		if strings.TrimSpace(s) == "" {
			return true
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
