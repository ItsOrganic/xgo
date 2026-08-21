// Command whackbench benchmarks whack against wgo and air: rebuild latency,
// cold-start latency, and resource usage, across a minimal and a realistic
// test app, in both a fair (equal debounce) and defaults (each tool's own
// out-of-the-box debounce) mode. Run from the benchmark/ directory:
//
//	cd benchmark && go run . --results results.json
//
// See BENCHMARKS.md at the repo root for the full methodology and results.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"whackbench/harness"
)

var readyTimeouts = map[string]time.Duration{
	"minimal":   10 * time.Second,
	"realistic": 30 * time.Second,
}

func main() {
	toolsFlag := flag.String("tool", "all", "Comma-separated tools to benchmark: whack,wgo,air or \"all\"")
	scenarioFlag := flag.String("scenario", "all", "Comma-separated scenarios: minimal,realistic or \"all\"")
	modeFlag := flag.String("mode", "all", "Comma-separated modes: fair (equal debounce across tools),defaults (each tool's own out-of-the-box debounce) or \"all\"")
	resultsPath := flag.String("results", "results.json", "Path to the results JSON file")
	resume := flag.Bool("resume", false, "Skip (tool, scenario) pairs already present in --results")
	rebuildTrials := flag.Int("rebuild-trials", 20, "Simulated edits per (tool, scenario) for rebuild latency")
	rebuildWarmup := flag.Int("rebuild-warmup", 3, "Rebuild-latency trials discarded as warmup")
	startupTrials := flag.Int("startup-trials", 8, "Cold-start trials per (tool, scenario)")
	startupWarmup := flag.Int("startup-warmup", 1, "Cold-start trials discarded as warmup")
	editGap := flag.Duration("edit-gap", 400*time.Millisecond, "Gap between simulated edits (must clear every tool's debounce window)")
	sampleInterval := flag.Duration("sample-interval", 200*time.Millisecond, "Resource sampling interval during the rebuild-latency loop")
	whackBinFlag := flag.String("whack-bin", "", "Path to a pre-built whack binary (default: build fresh from ..)")
	wgoBinFlag := flag.String("wgo-bin", "", "Path to the wgo binary (default: look up on PATH, then $HOME/go/bin/wgo)")
	airBinFlag := flag.String("air-bin", "", "Path to the air binary (default: look up on PATH, then $HOME/go/bin/air)")
	flag.Parse()

	if _, err := os.Stat("testapps"); err != nil {
		log.Fatalf("must be run from the benchmark/ directory (testapps/ not found here): %v", err)
	}

	tools := splitOrAll(*toolsFlag, []string{"whack", "wgo", "air"})
	scenarios := splitOrAll(*scenarioFlag, []string{"minimal", "realistic"})
	modes := splitOrAll(*modeFlag, []string{"fair", "defaults"})

	whackBin, err := resolveWhackBin(*whackBinFlag)
	if err != nil {
		log.Fatalf("resolve whack binary: %v", err)
	}
	wgoBin, err := resolveBin(*wgoBinFlag, "wgo")
	if err != nil {
		log.Fatalf("resolve wgo binary: %v", err)
	}
	airBin, err := resolveBin(*airBinFlag, "air")
	if err != nil {
		log.Fatalf("resolve air binary: %v", err)
	}
	log.Printf("using whack=%s wgo=%s air=%s", whackBin, wgoBin, airBin)

	resultSet, err := harness.LoadResultSet(*resultsPath)
	if err != nil {
		log.Fatalf("load results: %v", err)
	}

	for _, scenario := range scenarios {
		scenarioDir, err := filepath.Abs(filepath.Join("testapps", scenario))
		if err != nil {
			log.Fatalf("resolve scenario dir: %v", err)
		}
		markerPath := filepath.Join(scenarioDir, "marker.go")
		readyTimeout := readyTimeouts[scenario]

		for _, mode := range modes {
			for _, tool := range tools {
				tag := tool + "/" + scenario + "/" + mode
				if *resume && resultSet.Has(tool, scenario, mode) {
					log.Printf("[%s] skipping (already in %s, --resume)", tag, *resultsPath)
					continue
				}

				log.Printf("[%s] starting", tag)
				spec, err := buildToolSpec(tool, scenario, mode, scenarioDir, whackBin, wgoBin, airBin)
				if err != nil {
					log.Fatalf("[%s] build tool spec: %v", tag, err)
				}

				if err := resetScenarioState(scenarioDir, markerPath); err != nil {
					log.Fatalf("[%s] reset state: %v", tag, err)
				}

				log.Printf("[%s] cold-start: %d trials", tag, *startupTrials)
				coldRawAll, coldFailures := harness.MeasureColdStart(spec, *startupTrials, readyTimeout)
				coldSummary := harness.Summarize(harness.DropWarmup(coldRawAll, *startupWarmup))
				log.Printf("[%s] cold-start median=%.1fms p95=%.1fms failures=%d", tag, coldSummary.Median, coldSummary.P95, coldFailures)

				if err := resetScenarioState(scenarioDir, markerPath); err != nil {
					log.Fatalf("[%s] reset state before rebuild-latency phase: %v", tag, err)
				}

				log.Printf("[%s] rebuild-latency: %d trials", tag, *rebuildTrials)
				rebuildResult, err := harness.MeasureRebuildLatency(spec, markerPath, *rebuildTrials, *editGap, readyTimeout, *sampleInterval)
				if err != nil {
					log.Fatalf("[%s] rebuild-latency measurement: %v", tag, err)
				}
				rebuildSummary := harness.Summarize(harness.DropWarmup(rebuildResult.RawMs, *rebuildWarmup))
				log.Printf("[%s] rebuild-latency median=%.1fms p95=%.1fms failures=%d avgRSS=%.0fKiB avgCPU=%.1f%%",
					tag, rebuildSummary.Median, rebuildSummary.P95, rebuildResult.Failures,
					rebuildResult.Resource.AvgRSSKiB, rebuildResult.Resource.AvgCPUPercent)

				result := harness.ToolResult{
					Tool:                tool,
					Scenario:            scenario,
					Mode:                mode,
					Timestamp:           time.Now(),
					RebuildLatencyMsRaw: rebuildResult.RawMs,
					RebuildLatencyMs:    rebuildSummary,
					RebuildFailures:     rebuildResult.Failures,
					StartupLatencyMsRaw: coldRawAll,
					StartupLatencyMs:    coldSummary,
					StartupFailures:     coldFailures,
					AvgMemoryKiB:        rebuildResult.Resource.AvgRSSKiB,
					PeakMemoryKiB:       rebuildResult.Resource.PeakRSSKiB,
					AvgCPUPercent:       rebuildResult.Resource.AvgCPUPercent,
				}
				if err := resultSet.Add(result); err != nil {
					log.Fatalf("[%s] save result: %v", tag, err)
				}

				if err := resetScenarioState(scenarioDir, markerPath); err != nil {
					log.Printf("[%s] warning: cleanup after run failed: %v", tag, err)
				}
				log.Printf("[%s] done, saved to %s", tag, *resultsPath)
			}
		}
	}

	log.Printf("benchmark complete: %d results in %s", len(resultSet.Results), *resultsPath)
}

// resetScenarioState removes any leftover build output and resets the
// marker back to BuildMarker=0, so each phase starts from a clean, known
// state regardless of what the previous tool/phase left behind.
func resetScenarioState(scenarioDir, markerPath string) error {
	if err := os.RemoveAll(filepath.Join(scenarioDir, "tmp")); err != nil {
		return fmt.Errorf("remove tmp/: %w", err)
	}
	if err := harness.WriteMarker(markerPath, 0); err != nil {
		return fmt.Errorf("reset marker: %w", err)
	}
	return nil
}

func splitOrAll(flagVal string, all []string) []string {
	if flagVal == "" || flagVal == "all" {
		return all
	}
	parts := strings.Split(flagVal, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// resolveWhackBin builds a fresh whack binary from the repo root (one directory
// up from benchmark/) unless an existing binary path was given, so the
// benchmark always measures the current source, not a stale local install.
func resolveWhackBin(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	out, err := filepath.Abs(filepath.Join(os.TempDir(), "whackbench-whack-bin"))
	if err != nil {
		return "", err
	}
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = ".."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build whack from repo root: %w", err)
	}
	return out, nil
}

// resolveBin looks up name on PATH, then falls back to $HOME/go/bin/name
// (go install's default GOBIN when GOBIN/GOPATH aren't customized), since a
// freshly `go install`-ed tool commonly isn't on PATH yet in the same shell.
func resolveBin(explicit, name string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%s not on PATH and could not determine home dir for fallback: %w", name, err)
	}
	fallback := filepath.Join(home, "go", "bin", name)
	if _, err := os.Stat(fallback); err != nil {
		return "", fmt.Errorf("%s not on PATH and not found at %s (install with `go install`, or pass --%s-bin)", name, fallback, name)
	}
	return fallback, nil
}

// buildToolSpec constructs the launch command for tool/scenario/mode. In
// "fair" mode every tool is forced to the same 200ms debounce/delay, to
// isolate each tool's own architectural overhead. In "defaults" mode each
// tool is left at its own out-of-the-box debounce/delay - whack 50ms, wgo
// 300ms, air 1000ms - since that's what a user actually feels day to day
// without tuning anything.
func buildToolSpec(tool, scenario, mode, scenarioDir, whackBin, wgoBin, airBin string) (harness.ToolSpec, error) {
	configsDir, err := filepath.Abs("configs")
	if err != nil {
		return harness.ToolSpec{}, err
	}
	suffix := ""
	if mode == "defaults" {
		suffix = "-defaults"
	}

	switch tool {
	case "whack":
		cfgPath := filepath.Join(configsDir, "whack-"+scenario+suffix+".yaml")
		return harness.ToolSpec{
			Name: "whack",
			NewCmd: func() *exec.Cmd {
				cmd := exec.Command(whackBin, "run", "--config", cfgPath)
				cmd.Dir = scenarioDir
				return cmd
			},
		}, nil
	case "air":
		cfgPath := filepath.Join(configsDir, "air-"+scenario+suffix+".toml")
		return harness.ToolSpec{
			Name: "air",
			NewCmd: func() *exec.Cmd {
				cmd := exec.Command(airBin, "-c", cfgPath)
				cmd.Dir = scenarioDir
				return cmd
			},
		}, nil
	case "wgo":
		// wgo has no config file, so these flags mirror whack/air's
		// include/exclude settings directly. In "defaults" mode -debounce is
		// omitted entirely so wgo falls back to its own built-in 300ms.
		args := []string{"-file", ".go", "-xdir", "tmp"}
		if mode == "fair" {
			args = append(args, "-debounce", "200ms")
		}
		args = append(args, "go", "build", "-o", "./tmp/wgo-app", ".", "::", "./tmp/wgo-app")
		return harness.ToolSpec{
			Name: "wgo",
			NewCmd: func() *exec.Cmd {
				cmd := exec.Command(wgoBin, args...)
				cmd.Dir = scenarioDir
				return cmd
			},
		}, nil
	default:
		return harness.ToolSpec{}, fmt.Errorf("unknown tool %q (want whack, wgo, or air)", tool)
	}
}
