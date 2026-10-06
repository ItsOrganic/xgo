package harness

import (
	"path/filepath"
	"testing"
)

func TestLoadResultSet_MissingFileReturnsEmpty(t *testing.T) {
	rs, err := LoadResultSet(filepath.Join(t.TempDir(), "results.json"))
	if err != nil {
		t.Fatalf("LoadResultSet on missing file: %v", err)
	}
	if len(rs.Results) != 0 {
		t.Errorf("expected empty result set, got %d results", len(rs.Results))
	}
}

func TestAdd_PersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	rs, err := LoadResultSet(path)
	if err != nil {
		t.Fatal(err)
	}

	want := ToolResult{
		Tool:                "xgo",
		Scenario:            "minimal",
		RebuildLatencyMsRaw: []float64{10, 12, 11},
		RebuildLatencyMs:    Summarize([]float64{10, 12, 11}),
	}
	if err := rs.Add(want); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reloaded, err := LoadResultSet(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Results) != 1 {
		t.Fatalf("expected 1 result after reload, got %d", len(reloaded.Results))
	}
	got := reloaded.Results[0]
	if got.Tool != want.Tool || got.Scenario != want.Scenario {
		t.Errorf("got %+v, want tool/scenario %s/%s", got, want.Tool, want.Scenario)
	}
	if got.RebuildLatencyMs.Median != want.RebuildLatencyMs.Median {
		t.Errorf("median not round-tripped: got %v want %v", got.RebuildLatencyMs.Median, want.RebuildLatencyMs.Median)
	}
}

func TestHas_ResumeLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	rs, err := LoadResultSet(path)
	if err != nil {
		t.Fatal(err)
	}

	if rs.Has("xgo", "minimal", "fair") {
		t.Fatal("Has should be false before any Add")
	}
	if err := rs.Add(ToolResult{Tool: "xgo", Scenario: "minimal", Mode: "fair"}); err != nil {
		t.Fatal(err)
	}
	if !rs.Has("xgo", "minimal", "fair") {
		t.Error("Has should be true after Add for the same triple")
	}
	if rs.Has("xgo", "realistic", "fair") {
		t.Error("Has should be false for a different scenario of the same tool")
	}
	if rs.Has("air", "minimal", "fair") {
		t.Error("Has should be false for a different tool of the same scenario")
	}
	if rs.Has("xgo", "minimal", "defaults") {
		t.Error("Has should be false for a different mode of the same tool/scenario")
	}
}

func TestAdd_SameKeyOverwritesInsteadOfDuplicating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	rs, err := LoadResultSet(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := rs.Add(ToolResult{Tool: "xgo", Scenario: "minimal", Mode: "fair", AvgMemoryKiB: 100}); err != nil {
		t.Fatal(err)
	}
	if err := rs.Add(ToolResult{Tool: "xgo", Scenario: "minimal", Mode: "fair", AvgMemoryKiB: 200}); err != nil {
		t.Fatal(err)
	}

	if len(rs.Results) != 1 {
		t.Fatalf("re-adding the same (tool, scenario, mode) should overwrite, not duplicate: got %d entries", len(rs.Results))
	}
	if rs.Results[0].AvgMemoryKiB != 200 {
		t.Errorf("expected the overwritten value 200, got %v", rs.Results[0].AvgMemoryKiB)
	}
}

func TestAdd_DifferentModeSameToolScenarioDoesNotOverwrite(t *testing.T) {
	// Regression guard for the "fair" vs "defaults" comparison: two entries
	// that share tool+scenario but differ only in mode must both survive.
	path := filepath.Join(t.TempDir(), "results.json")
	rs, err := LoadResultSet(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := rs.Add(ToolResult{Tool: "xgo", Scenario: "minimal", Mode: "fair"}); err != nil {
		t.Fatal(err)
	}
	if err := rs.Add(ToolResult{Tool: "xgo", Scenario: "minimal", Mode: "defaults"}); err != nil {
		t.Fatal(err)
	}
	if len(rs.Results) != 2 {
		t.Fatalf("expected 2 distinct entries (different modes), got %d", len(rs.Results))
	}
}
