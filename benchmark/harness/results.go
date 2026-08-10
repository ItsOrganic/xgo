package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ToolResult is everything measured for one (tool, scenario, mode) triple.
// Mode is "fair" (every tool forced to the same debounce/delay, isolating
// each tool's own architectural overhead) or "defaults" (each tool left at
// its own out-of-the-box debounce/delay, showing what a user actually
// feels day to day without tuning anything).
type ToolResult struct {
	Tool      string    `json:"tool"`
	Scenario  string    `json:"scenario"`
	Mode      string    `json:"mode"`
	Timestamp time.Time `json:"timestamp"`

	RebuildLatencyMsRaw []float64 `json:"rebuild_latency_ms_raw"`
	RebuildLatencyMs    Summary   `json:"rebuild_latency_ms"`
	RebuildFailures     int       `json:"rebuild_failures"`

	StartupLatencyMsRaw []float64 `json:"startup_latency_ms_raw"`
	StartupLatencyMs    Summary   `json:"startup_latency_ms"`
	StartupFailures     int       `json:"startup_failures"`

	AvgMemoryKiB  float64 `json:"avg_memory_kib"`
	PeakMemoryKiB float64 `json:"peak_memory_kib"`
	AvgCPUPercent float64 `json:"avg_cpu_percent"`
}

// Key identifies a (tool, scenario, mode) triple for resume/lookup purposes.
func (r ToolResult) Key() string {
	return r.Tool + "/" + r.Scenario + "/" + r.Mode
}

// ResultSet is the full output of a benchmark run: a JSON file that's
// rewritten atomically after every completed (tool, scenario) pair, so a
// crash partway through a multi-hour run loses at most the in-flight pair.
type ResultSet struct {
	mu      sync.Mutex
	path    string
	Results []ToolResult `json:"results"`
}

// LoadResultSet loads path if it exists, or returns an empty ResultSet
// bound to path (created on the first Add) if it doesn't.
func LoadResultSet(path string) (*ResultSet, error) {
	rs := &ResultSet{path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return rs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read results file %s: %w", path, err)
	}
	if err := json.Unmarshal(data, rs); err != nil {
		return nil, fmt.Errorf("parse results file %s: %w", path, err)
	}
	rs.path = path // json.Unmarshal only touches the exported Results field
	return rs, nil
}

// Has reports whether a result for (tool, scenario, mode) is already
// recorded, for --resume to skip completed triples.
func (rs *ResultSet) Has(tool, scenario, mode string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	want := tool + "/" + scenario + "/" + mode
	for _, r := range rs.Results {
		if r.Key() == want {
			return true
		}
	}
	return false
}

// Add appends result (replacing any existing entry for the same
// tool/scenario, so a --resume run that re-does a pair overwrites cleanly)
// and immediately persists the whole set to disk.
func (rs *ResultSet) Add(result ToolResult) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	replaced := false
	for i, r := range rs.Results {
		if r.Key() == result.Key() {
			rs.Results[i] = result
			replaced = true
			break
		}
	}
	if !replaced {
		rs.Results = append(rs.Results, result)
	}
	return rs.saveLocked()
}

// saveLocked writes the result set to disk atomically (write to a temp file
// in the same directory, then rename) so a crash mid-write never leaves a
// truncated/corrupt results.json behind. Caller must hold rs.mu.
func (rs *ResultSet) saveLocked() error {
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	dir := filepath.Dir(rs.path)
	tmp, err := os.CreateTemp(dir, ".results-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp results file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp results file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp results file: %w", err)
	}
	if err := os.Rename(tmpPath, rs.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp results file into place: %w", err)
	}
	return nil
}
