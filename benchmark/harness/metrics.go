// Package harness implements the benchmark orchestration: driving each
// hot-reload tool as a subprocess, detecting rebuild-ready signals, sampling
// resource usage, and reducing raw samples to reportable statistics.
package harness

import (
	"math"
	"sort"
)

// Summary is the set of descriptive statistics reported for one metric
// (e.g. rebuild latency, memory) over one (tool, scenario) pair.
type Summary struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	StdDev float64 `json:"std_dev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// Summarize reduces vals to a Summary. Returns the zero Summary for an
// empty input.
func Summarize(vals []float64) Summary {
	if len(vals) == 0 {
		return Summary{}
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	return Summary{
		N:      len(sorted),
		Mean:   mean(sorted),
		Median: percentileSorted(sorted, 50),
		P95:    percentileSorted(sorted, 95),
		StdDev: stdDev(sorted),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
	}
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func stdDev(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := mean(vals)
	var sumSq float64
	for _, v := range vals {
		d := v - m
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(vals)))
}

// percentileSorted returns the p-th percentile (0-100) of an already-sorted
// slice, using linear interpolation between closest ranks.
func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// DropWarmup returns vals with the first n elements removed. If n >=
// len(vals), returns an empty (non-nil) slice rather than panicking, so
// callers can safely Summarize a too-short run and get a zero Summary.
func DropWarmup(vals []float64, n int) []float64 {
	if n >= len(vals) {
		return []float64{}
	}
	if n <= 0 {
		return vals
	}
	out := make([]float64, len(vals)-n)
	copy(out, vals[n:])
	return out
}
