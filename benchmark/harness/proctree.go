package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clockTicksPerSecond is Linux's USER_HZ, used to convert /proc/<pid>/stat's
// utime/stime (in clock ticks) into wall-clock time. 100 is the practically
// universal value on x86_64 Linux distros (it's a kernel compile-time
// constant, not something that varies per-machine in normal deployments) -
// hardcoded rather than pulled in via a cgo/sysconf dependency to keep this
// module dependency-free.
const clockTicksPerSecond = 100

// Descendants returns every PID whose process tree is rooted at pid
// (children, grandchildren, and so on), by scanning /proc for the current
// PPID relationships. Does not include pid itself. Best-effort: processes
// that exit while this function runs are simply absent from the result
// rather than causing an error, since sampling a live process tree
// inherently races process churn.
func Descendants(pid int) []int {
	childrenOf := buildParentIndex()

	var out []int
	queue := []int{pid}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, child := range childrenOf[p] {
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}

// buildParentIndex scans /proc once and returns a map from PID to its
// direct children's PIDs.
func buildParentIndex() map[int][]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	index := make(map[int][]int)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a PID directory (e.g. "self", "net", ...)
		}
		ppid, ok := readPPID(pid)
		if !ok {
			continue // process exited between ReadDir and read; skip it
		}
		index[ppid] = append(index[ppid], pid)
	}
	return index
}

// readPPID parses the parent PID out of /proc/<pid>/stat. The format is
// "pid (comm) state ppid ...", where comm may itself contain spaces or
// parentheses, so we locate the *last* ')' before splitting the remainder
// on whitespace.
func readPPID(pid int) (int, bool) {
	fields, ok := readStatFields(pid)
	if !ok || len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// readStatFields returns the whitespace-split fields of /proc/<pid>/stat
// *after* the "pid (comm) " prefix, so fields[0] is process state, fields[1]
// is ppid, fields[11] is utime, fields[12] is stime (per proc(5)).
func readStatFields(pid int) ([]string, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil, false
	}
	closeParen := bytes.LastIndexByte(data, ')')
	if closeParen < 0 || closeParen+2 > len(data) {
		return nil, false
	}
	return strings.Fields(string(data[closeParen+2:])), true
}

// readCPUTicks returns utime+stime (in clock ticks) for pid.
func readCPUTicks(pid int) (int64, bool) {
	fields, ok := readStatFields(pid)
	if !ok || len(fields) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseInt(fields[11], 10, 64)
	stime, err2 := strconv.ParseInt(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return utime + stime, true
}

// readRSSKiB returns VmRSS for pid in KiB, parsed from /proc/<pid>/status.
func readRSSKiB(pid int) (int64, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb, true
	}
	return 0, false
}

// ProcessesInGroup returns every PID currently in process group pgid.
// Unlike Descendants, this doesn't rely on parent-child relationships, so
// it still finds processes that have been reparented (typically to init)
// after their original parent already exited - exactly the case for
// stragglers left behind by a killed process group, since a PGID broadcast
// signal doesn't terminate every member atomically.
func ProcessesInGroup(pgid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a PID directory
		}
		fields, ok := readStatFields(pid)
		if !ok || len(fields) < 3 {
			continue // process exited between ReadDir and read
		}
		g, err := strconv.Atoi(fields[2]) // fields[2] is pgrp, per proc(5)
		if err != nil || g != pgid {
			continue
		}
		out = append(out, pid)
	}
	return out
}

// TreeSnapshot sums RSS and CPU ticks across root and all of its current
// descendants. Individual processes that have already exited by the time
// they're read are skipped, not treated as an error.
func TreeSnapshot(root int) (rssKiB, cpuTicks int64) {
	pids := append([]int{root}, Descendants(root)...)
	for _, pid := range pids {
		if v, ok := readRSSKiB(pid); ok {
			rssKiB += v
		}
		if v, ok := readCPUTicks(pid); ok {
			cpuTicks += v
		}
	}
	return rssKiB, cpuTicks
}

// ResourceSampler periodically snapshots a process tree's memory and CPU
// usage until Stop is called, then reduces the samples to averages/peaks.
type ResourceSampler struct {
	root     int
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}

	mu            sync.Mutex
	rssSamplesKiB []float64
	cpuPercents   []float64
}

// NewResourceSampler creates a sampler for the process tree rooted at root,
// snapshotting every interval once Start is called.
func NewResourceSampler(root int, interval time.Duration) *ResourceSampler {
	return &ResourceSampler{
		root:     root,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins sampling in a background goroutine. Safe to call once.
func (s *ResourceSampler) Start() {
	go func() {
		defer close(s.doneCh)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		_, prevCPUTicks := TreeSnapshot(s.root)
		prevTime := time.Now()

		for {
			select {
			case <-s.stopCh:
				return
			case now := <-ticker.C:
				rss, cpuTicks := TreeSnapshot(s.root)
				elapsed := now.Sub(prevTime).Seconds()
				deltaTicks := cpuTicks - prevCPUTicks
				var cpuPercent float64
				if elapsed > 0 {
					cpuPercent = (float64(deltaTicks) / clockTicksPerSecond) / elapsed * 100
					if cpuPercent < 0 {
						// A transient child (e.g. a `go build` subprocess)
						// can exit between two samples, briefly making the
						// summed ticks across the tree dip versus the prior
						// sample. CPU% is a rate; it can't be negative.
						cpuPercent = 0
					}
				}
				prevCPUTicks = cpuTicks
				prevTime = now

				s.mu.Lock()
				s.rssSamplesKiB = append(s.rssSamplesKiB, float64(rss))
				s.cpuPercents = append(s.cpuPercents, cpuPercent)
				s.mu.Unlock()
			}
		}
	}()
}

// ResourceResult is the reduced output of a ResourceSampler run.
type ResourceResult struct {
	AvgRSSKiB     float64
	PeakRSSKiB    float64
	AvgCPUPercent float64
}

// Stop halts sampling and returns the reduced averages/peak. Blocks until
// the sampling goroutine has actually stopped, so it's safe to read process
// state immediately after Stop returns.
func (s *ResourceSampler) Stop() ResourceResult {
	close(s.stopCh)
	<-s.doneCh

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.rssSamplesKiB) == 0 {
		return ResourceResult{}
	}
	var sumRSS, peakRSS, sumCPU float64
	for i, v := range s.rssSamplesKiB {
		sumRSS += v
		if v > peakRSS {
			peakRSS = v
		}
		sumCPU += s.cpuPercents[i]
	}
	n := float64(len(s.rssSamplesKiB))
	return ResourceResult{
		AvgRSSKiB:     sumRSS / n,
		PeakRSSKiB:    peakRSS,
		AvgCPUPercent: sumCPU / n,
	}
}
