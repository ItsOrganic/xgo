package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
			Cmd:     "go build -o ./tmp/xgo-app .",
			Timeout: 30 * time.Second,
		},
		Run: RunConfig{
			Cmd: "./tmp/xgo-app",
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

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
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
	if cfg.Watch.Debounce <= 0 {
		cfg.Watch.Debounce = 50 * time.Millisecond
	}
	if cfg.Build.Cmd == "" {
		cfg.Build.Cmd = "go build -o ./tmp/xgo-app ."
	}
	if cfg.Build.Timeout <= 0 {
		cfg.Build.Timeout = 30 * time.Second
	}
	if cfg.Run.Cmd == "" {
		cfg.Run.Cmd = "./tmp/xgo-app"
	}
	if cfg.Log.Prefix == "" {
		cfg.Log.Prefix = "[xgo]"
	}
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
