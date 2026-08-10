package harness

import (
	"math"
	"testing"
)

func floatsClose(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func TestSummarize_KnownVector(t *testing.T) {
	// 1..10: mean=5.5, stddev (population) ~= 2.8723, median=5.5
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s := Summarize(vals)

	if s.N != 10 {
		t.Errorf("N = %d, want 10", s.N)
	}
	if !floatsClose(s.Mean, 5.5, 1e-9) {
		t.Errorf("Mean = %v, want 5.5", s.Mean)
	}
	if !floatsClose(s.Median, 5.5, 1e-9) {
		t.Errorf("Median = %v, want 5.5", s.Median)
	}
	if !floatsClose(s.StdDev, 2.8722813232690143, 1e-9) {
		t.Errorf("StdDev = %v, want ~2.8722813232690143", s.StdDev)
	}
	if s.Min != 1 || s.Max != 10 {
		t.Errorf("Min/Max = %v/%v, want 1/10", s.Min, s.Max)
	}
	// p95 of 1..10 via linear interpolation at rank 0.95*9=8.55 -> between
	// sorted[8]=9 and sorted[9]=10, frac 0.55 -> 9.55
	if !floatsClose(s.P95, 9.55, 1e-9) {
		t.Errorf("P95 = %v, want 9.55", s.P95)
	}
}

func TestSummarize_Empty(t *testing.T) {
	s := Summarize(nil)
	if s != (Summary{}) {
		t.Errorf("Summarize(nil) = %+v, want zero value", s)
	}
}

func TestSummarize_SingleValue(t *testing.T) {
	s := Summarize([]float64{42})
	if s.Mean != 42 || s.Median != 42 || s.P95 != 42 || s.Min != 42 || s.Max != 42 {
		t.Errorf("single-value summary should collapse to the value everywhere: %+v", s)
	}
	if s.StdDev != 0 {
		t.Errorf("StdDev of single value should be 0, got %v", s.StdDev)
	}
}

func TestDropWarmup(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}
	got := DropWarmup(vals, 3)
	want := []float64{4, 5}
	if len(got) != len(want) {
		t.Fatalf("DropWarmup(3) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("DropWarmup(3)[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestDropWarmup_MoreThanLen(t *testing.T) {
	got := DropWarmup([]float64{1, 2}, 5)
	if len(got) != 0 {
		t.Errorf("DropWarmup with n >= len should return empty, got %v", got)
	}
}

func TestDropWarmup_MutationSafety(t *testing.T) {
	// DropWarmup must not let the caller's later mutation of the input
	// affect the returned slice, and vice versa - it must copy, not reslice.
	vals := []float64{1, 2, 3, 4, 5}
	got := DropWarmup(vals, 2)
	got[0] = 999
	if vals[2] == 999 {
		t.Errorf("DropWarmup result shares backing array with input")
	}
}
