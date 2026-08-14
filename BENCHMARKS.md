# xgo Benchmarks: xgo vs wgo vs air

Measured, not asserted. This compares `xgo` against two established Go
hot-reloaders, [`wgo`](https://github.com/bokwoon95/wgo) and
[`air`](https://github.com/air-verse/air), across rebuild latency, cold-start
latency, and resource usage — in **two modes**, because the two tell
different stories:

- **`fair`** — every tool forced to the same 200ms debounce/delay and the
  same build command. Isolates each tool's own architectural overhead from
  its shipped configuration.
- **`defaults`** — each tool left exactly as it ships. Shows what you
  actually feel day to day if you never tune anything.

The harness, raw data, and full methodology are in
[`benchmark/`](benchmark/) — every number below is reproducible with one
command.

## TL;DR

- **`defaults` mode: xgo is substantially faster out of the box.** Median
  rebuild latency is **~1.85x faster than wgo** and **~3.9x faster than
  air**, consistently across both scenarios and with tight p95s.
- **Cold start is now a tie with wgo** (~150ms vs ~150ms) and **~2.2x
  faster than air**. This used to be xgo's clear loss — see
  [What changed](#what-changed-since-the-previous-run) below.
- **`fair` mode: rebuild latency is a statistical tie** across all three
  tools, in both scenarios. Held to the same debounce and the same build
  command, the three engines do not meaningfully differ. That is the honest
  read: xgo's `defaults`-mode win is a *configuration* win, not a claim that
  its watcher is architecturally faster.
- **wgo is still the leanest on memory** at steady state (~31-34 MiB vs
  xgo's ~39-43 MiB). air is heaviest in `fair` mode; xgo and air are close
  in `defaults` mode.
- Zero failed trials across all 336 measured runs in the final set.

## Methodology

**Machine**: AMD Ryzen 5 5600H (12 threads), 13GiB RAM, Kali GNU/Linux
Rolling (Linux 6.6.15), Go 1.23.0, linux/amd64. Single machine — see
[Limitations](#limitations) for what that does and doesn't support claiming.

**Tool versions**: `xgo` built from this repo's current source
(`benchmark/` builds it fresh on every run, so it always measures the
committed code, never a stale local install). `wgo` v0.6.4. `air` v1.67.4.

**Test apps** (`benchmark/testapps/`):
- **minimal**: one file, one dependency-free HTTP server. Isolates each
  tool's own watch/restart overhead from Go compiler speed.
- **realistic**: ~5 packages (an LRU cache, string utils, math utils, and a
  service layer combining them), a few hundred LOC.

Both apps print `READY build=<N> ts=<unixnano>` the instant they're bound
and about to accept connections. This is the only thing the benchmark
harness watches for on each tool's stdout — detection logic is identical
regardless of which tool is wrapping the process.

**The two modes, concretely.** `fair` mode gives all three tools a 200ms
debounce/delay *and the identical build command* `go build -o ./tmp/X-app .`.
`defaults` mode omits the debounce key so each tool falls back to its own
built-in (xgo 50ms, wgo 300ms, air 1000ms), and gives xgo the build command
it actually ships (`config.DefaultBuildCmd`, which passes
`-ldflags="-s -w"`). Those stripping flags are a shipped default in exactly
the same sense as the debounce value — nothing stops a wgo or air user from
passing them too — so they belong in `defaults` mode and are deliberately
kept out of `fair` mode.

**Measurements**, per (tool, scenario, mode) triple:
1. **Cold-start latency**: 8 full launch→first-ready cycles, discard the
   first as warmup, report median/p95 of the remaining 7.
2. **Rebuild latency**: start the tool once, confirm the first build comes
   up (not counted — that's cold-start), then make 20 simulated edits
   400ms apart (rewriting a `BuildMarker` constant), timing edit-to-ready
   for each. Discard the first 3 as warmup, report median/p95 of the
   remaining 17.
3. **Resource usage**: sampled every 200ms during the rebuild-latency loop
   only, walking `/proc` for the tool's *entire* process tree and summing
   RSS and CPU time. Peak RSS is dominated by the `go build` compiler
   subprocess, not steady-state idle memory.

**Reproduce it**:
```bash
cd benchmark
go run . --results results.json
```
Runs all 12 (tool × scenario × mode) combinations, about 7 minutes on the
machine above. `results.json` has every raw measurement, not just the
summary stats below.

## Results — defaults mode (each tool exactly as it ships)

### Minimal scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **340.0** | 628.8 | 1322.7 |
| Rebuild latency p95 (ms) | **370.9** | 649.5 | 1339.9 |
| Cold-start median (ms) | 150.4 | 149.3 | 330.4 |
| Avg memory (MiB) | 38.6 | **33.0** | 42.9 |
| Avg CPU during rebuild loop | 32.0% | 32.7% | 18.6% |

### Realistic scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **342.9** | 634.1 | 1331.9 |
| Rebuild latency p95 (ms) | **391.5** | 647.5 | 1361.0 |
| Cold-start median (ms) | 155.1 | 147.9 | 342.0 |
| Avg memory (MiB) | 39.0 | **31.0** | 39.6 |
| Avg CPU during rebuild loop | 32.8% | 31.8% | 18.2% |

Two things drive xgo's rebuild win here, both shipped defaults: a 50ms
debounce against wgo's 300ms and air's 1000ms, and `-ldflags="-s -w"`,
which drops the symbol table and DWARF info that the reload loop never
reads. Linking is the most expensive phase of a rebuild, and skipping that
output measures at ~124ms saved per build in isolation.

Note air's *lower* CPU% is a direct consequence of its 1000ms delay — it is
doing less work per second specifically because it is reacting a full
second later, not because it is more efficient per rebuild. See
[A note on the CPU metric](#a-note-on-the-cpu-metric).

## Results — fair mode (equal debounce, identical build command)

### Minimal scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 562.8 | 549.7 | 559.6 |
| Rebuild latency p95 (ms) | 580.3 | 572.5 | 575.8 |
| Cold-start median (ms) | 162.3 | 152.8 | 350.4 |
| Avg memory (MiB) | 41.7 | **34.5** | 52.0 |
| Avg CPU during rebuild loop | 34.8% | 34.9% | 34.7% |

### Realistic scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 550.7 | 531.1 | 528.4 |
| Rebuild latency p95 (ms) | 578.9 | 552.3 | 543.1 |
| Cold-start median (ms) | 145.6 | 153.0 | 324.5 |
| Avg memory (MiB) | 42.8 | **33.9** | 50.0 |
| Avg CPU during rebuild loop | 33.7% | 36.6% | 33.7% |

Rebuild-latency gaps here (≤22ms) are inside run-to-run variance for every
tool — a genuine tie, not a close win.

## Interpretation

**Rebuild latency ties in `fair` mode because `go build` dominates.** A
breakdown of one edit-to-ready cycle on this machine: xgo's own startup
(config load, watcher walk, fingerprint priming) is ~10ms, stopping the old
process and starting the new one is ~10ms, and essentially everything else
is the compiler. Of that, the link step alone is ~350-400ms. Once every tool
is held to the same debounce and the same build command, there is very
little left for a hot-reload tool to differentiate on.

**`defaults` mode is what you actually experience**, and it is not close.
Both of xgo's advantages there are honest, reproducible configuration
choices, and both are things a wgo or air user could adopt — xgo's claim is
that you get them without having to know to ask.

**Cold start is where the artifact lifecycle matters.** `go build -o X`
only takes its up-to-date fast path when `X` already exists; if it has to
link from scratch it costs roughly 2.4x as much. xgo and air both used to
delete their build output on exit (air still does, via `clean_on_exit`),
which meant paying a full re-link on every single start. That is the whole
explanation for air's ~330-342ms against wgo's and xgo's ~150ms.

**Memory: wgo leanest, consistently.** wgo's supervisor process idles at
~4.1 MiB against xgo's ~10.9 MiB and air's ~15.9 MiB. Reducing xgo's
supervisor footprint is open work.

## What changed since the previous run

The previous version of this report showed xgo cold-starting at ~630ms
against wgo's ~240ms, and flagged the cause as "observed, not root-caused."
It has since been root-caused and fixed: xgo deleted its own build output
on exit, so every run paid a full re-link. Removing that deletion moved
cold start from 630ms to 234ms in a controlled A/B on the same machine, and
it now measures ~150ms here.

Absolute numbers across the whole table are also lower than the previous
run — `fair`-mode rebuild latency is ~550ms here versus ~820ms before, for
all three tools including the two that did not change. That is background
system load, not a code change, and it is precisely why this report was
regenerated end-to-end rather than having new xgo numbers spliced into old
ones. Compare within a run, never across runs.

## A note on the CPU metric

Average CPU% structurally penalizes whichever tool is fastest: the same
compiler work compressed into a shorter window reads as a higher
percentage. air's 18.6% is not efficiency, it is a 1000ms delay. **Total
CPU-seconds per edit-to-ready cycle** would be the honest metric, and the
harness already reads cumulative ticks from `/proc`, so it is a subtraction
rather than a rate. Treat the CPU column here as directional only.

## Limitations

- **Single machine, single run.** Reproducible on this exact machine via
  the command above, not claimed as universally representative.
- **Linux only.** The harness relies on `/proc` and Unix process groups.
- **Small apps only.** Both scenarios are small enough that `go build` is
  fast. A large monorepo with a multi-second build would compress the
  `defaults`-mode gap, since debounce becomes a smaller fraction of a much
  longer total — untested here.
- **The `defaults` finding is a config-shipping decision**, not a claim
  about which engine is better. `fair` mode is the architecture comparison,
  and it is a tie.
- **A known bug affected data collection.** During this run, xgo
  intermittently leaked its app process on shutdown (orphaned, still
  holding its port), which caused `bind: address already in use` failures on
  subsequent trials. Affected combinations were re-run after clearing
  strays, and every number reported above comes from a clean run with zero
  failures — but the underlying bug is real and unfixed, and it is being
  tracked separately.
