package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	"xgo/internal/runner"
)

type runtimeState struct {
	UpdatedAt      time.Time           `yaml:"updated_at"`
	WatchedDirs    []string            `yaml:"watched_dirs"`
	Include        []string            `yaml:"include"`
	Exclude        []string            `yaml:"exclude"`
	Debounce       time.Duration       `yaml:"debounce"`
	PIDs           map[string]int      `yaml:"pids"`
	RestartCount   int                 `yaml:"restart_count"`
	LastRestart    time.Time           `yaml:"last_restart_time"`
	LastBuild      time.Duration       `yaml:"last_build_duration"`
	LastBuildError bool                `yaml:"last_build_failed"`
	FoundConfig    bool                `yaml:"found_config"`
	ConfigPath     string              `yaml:"config_path"`
	BuildCmd       string              `yaml:"build_cmd"`
	RunCmd         string              `yaml:"run_cmd"`
	ExtraCommands  []string            `yaml:"extra_cmds"`
	Hooks          map[string][]string `yaml:"hooks"`
}

func stateFilePath(wd string) string {
	return filepath.Join(wd, "tmp", "xgo-status.yaml")
}

func writeState(path string, st runtimeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	b, err := yaml.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func stateFromSnapshot(base runtimeState, snap runner.StatusSnapshot) runtimeState {
	base.UpdatedAt = time.Now()
	base.PIDs = snap.PIDs
	base.RestartCount = snap.RestartCount
	base.LastRestart = snap.LastRestartTime
	base.LastBuild = snap.LastBuild
	base.LastBuildError = snap.LastBuildFailed
	return base
}
