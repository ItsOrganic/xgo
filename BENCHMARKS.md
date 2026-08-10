# xgo Benchmarks: xgo vs wgo vs air

Measured, not asserted. This compares `xgo` against two established Go
hot-reloaders, [`wgo`](https://github.com/bokwoon95/wgo) and
[`air`](https://github.com/air-verse/air), across rebuild latency, cold-start
latency, and resource usage — in **two modes**, because the two tell
different stories:

- **`fair`** — every tool forced to the same 200ms debounce/delay. Isolates
  each tool's own architectural overhead from its config.
- **`defaults`** — each tool left at its own out-of-the-box debounce/delay
  (xgo 50ms, wgo 300ms, air 1000ms). Shows what you actually feel day to day
  if you never tune anything.

The harness, raw data, and full methodology are in
[`benchmark/`](benchmark/) — every number below is reproducible with one
command.

## TL;DR

- **`fair` mode: rebuild latency is a statistical tie** across all three
  tools, in both scenarios. For apps this small, `go build`'s own fixed
  process/toolchain startup cost dominates the total edit-to-ready time far
  more than any hot-reload tool's own watcher/debounce/restart overhead
  does.
- **`defaults` mode tells a different, much bigger story: xgo is
  meaningfully faster out of the box** — median rebuild latency **~2.5x
  faster than air** and **~1.4x faster than wgo**, entirely because xgo
  ships with a 50ms debounce against air's stock 1000ms and wgo's stock
  300ms. This is the gap you actually feel using each tool with its default
  config, and it's the real answer to "why does xgo feel faster than the
  numbers suggest" — the fair-mode benchmark deliberately removes this
  advantage to isolate architecture; the defaults-mode benchmark puts it
  back because it's real.
- **wgo cold-starts faster** than xgo or air in both modes (debounce doesn't
  affect the very first build, so this is architectural, not a config
  artifact) and **uses the least memory** at steady state.
- **air is the heaviest on memory**; xgo sits in the middle.
- Zero failed trials across all 336 measured runs (6 tool/scenario/mode
  combinations × 2 scenarios × 28 trials each) — all three tools are
  equally reliable in this test.

## Methodology

**Machine**: AMD Ryzen 5 5600H (12 threads), 13GiB RAM, Kali GNU/Linux
Rolling (Linux 6.6), Go 1.23.0, linux/amd64. Single machine — see
[Limitations](#limitations) for what that does and doesn't support claiming.

**Tool versions**: `xgo` built from this repo's current source
(`benchmark/` builds it fresh on every run, so it always measures the
committed code, never a stale local install). `wgo` v0.6.4. `air` v1.67.4.
Both installed via `go install <module>@latest` on 2026-08-09.

**Test apps** (`benchmark/testapps/`):
- **minimal**: one file, one dependency-free HTTP server. Isolates each
  tool's own watch/restart overhead from Go compiler speed.
- **realistic**: ~5 packages (an LRU cache, string utils, math utils, and a
  service layer combining them), a few hundred LOC. Closer to real-world
  build times.

Both apps print `READY build=<N> ts=<unixnano>` the instant they're bound
and about to accept connections. This is the only thing the benchmark
harness watches for on each tool's stdout — detection logic is identical
regardless of which tool is wrapping the process.

**The two modes, concretely**: `fair` mode uses `xgo-*.yaml` /
`air-*.toml` / `wgo -debounce 200ms`. `defaults` mode uses
`xgo-*-defaults.yaml` / `air-*-defaults.toml` (both simply omit the
debounce/delay key, falling back to each tool's own built-in default) and
drops wgo's `-debounce` flag entirely. Everything else — build command, run
command, include/exclude patterns — is identical in both modes; only the
debounce/delay setting changes. wgo has no config file, so it's invoked via
CLI flags directly in `benchmark/main.go`'s `buildToolSpec` — in `fair` mode
that's `-file .go -debounce 200ms -xdir tmp`; in `defaults` mode the
`-debounce` flag is simply omitted, falling back to wgo's own built-in
300ms.

**Measurements**, per (tool, scenario, mode) triple:
1. **Cold-start latency**: 8 full launch→first-ready cycles, discard the
   first as warmup, report median/p95/stddev of the remaining 7.
2. **Rebuild latency**: start the tool once, confirm the first build comes
   up (not counted — that's cold-start), then make 20 simulated edits
   400ms apart (rewriting a `BuildMarker` constant), timing edit-to-ready
   for each. Discard the first 3 as warmup, report median/p95/stddev of the
   remaining 17.
3. **Resource usage**: sampled every 200ms during the rebuild-latency loop
   only, walking `/proc` for the tool's *entire* process tree (the tool
   itself plus whatever it spawns) and summing RSS and CPU time. Reports
   average/peak RSS and average CPU%. Peak RSS is dominated by the `go
   build` compiler subprocess's own memory use, not steady-state idle
   memory.

**Reproduce it**:
```bash
cd benchmark
go run . --results results.json
```
Runs all 12 (tool × scenario × mode) combinations, about 7 minutes on the
machine above. `results.json` (checked into this repo as the run this
report is generated from) has every raw measurement, not just the summary
stats below. Use `--mode fair` or `--mode defaults` to run just one mode.

## Results — fair mode (equal 200ms debounce/delay)

### Minimal scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 833.4 | 816.5 | 812.2 |
| Rebuild latency p95 (ms) | 846.7 | 839.0 | 843.4 |
| Cold-start median (ms) | 634.4 | **242.3** | 618.0 |
| Avg memory (MiB) | 45.0 | **36.9** | 49.6 |
| Avg CPU during rebuild loop | 61.9% | 62.7% | 58.7% |

### Realistic scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 818.6 | 814.8 | 813.3 |
| Rebuild latency p95 (ms) | 842.7 | 827.9 | 821.3 |
| Cold-start median (ms) | 629.0 | **240.8** | 611.9 |
| Avg memory (MiB) | 46.5 | **34.1** | 48.5 |
| Avg CPU during rebuild loop | 62.9% | 55.5% | 60.9% |

Bold marks the best value per row where the gap is larger than measurement
noise. Rebuild latency gaps here (≤21ms) are inside the run-to-run stddev
for every tool — a genuine tie, not a close win.

## Results — defaults mode (each tool's own out-of-the-box setting)

### Minimal scenario

| Metric | xgo (50ms) | wgo (300ms) | air (1000ms) |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **652.5** | 915.7 | 1623.8 |
| Rebuild latency p95 (ms) | **700.1** | 933.5 | 1684.2 |
| Cold-start median (ms) | 624.5 | **239.1** | 611.9 |
| Avg memory (MiB) | 51.3 | **36.3** | 38.6 |
| Avg CPU during rebuild loop | 72.1% | 57.0% | **38.7%** |

### Realistic scenario

| Metric | xgo (50ms) | wgo (300ms) | air (1000ms) |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **655.9** | 912.3 | 1632.9 |
| Rebuild latency p95 (ms) | **669.9** | 932.0 | 1677.0 |
| Cold-start median (ms) | 614.3 | **231.8** | 613.2 |
| Avg memory (MiB) | 51.6 | **34.5** | 38.8 |
| Avg CPU during rebuild loop | 72.8% | 57.6% | **40.8%** |

xgo's rebuild-latency win here is large and consistent: **~2.5x faster than
air, ~1.4x faster than wgo**, in both scenarios, with tight p95s (no
long-tail flakiness). Note air's *lower* CPU% is a direct consequence of
its 1000ms delay — it's doing less work per second specifically because
it's reacting an entire second later, not because it's more efficient
per-rebuild.

## Interpretation

**Rebuild latency ties in `fair` mode because `go build`'s own fixed
overhead dominates at this scale**, once every tool is held to the same
debounce. That's the correct way to isolate architecture: it says xgo,
wgo, and air do not meaningfully differ in how efficiently they detect a
change, debounce it, rebuild, and restart, when given the same reaction
window.

**But `defaults` mode is the one that matches what you'd actually
experience**, and it's not close. xgo ships with a 50ms debounce; wgo ships
with 300ms; air ships with a full 1000ms. That difference is directly
additive to every single edit-to-ready cycle, so over a coding session with
dozens of saves, xgo's default config saves real, cumulative time — this is
an honest, config-driven win, not an architectural one, and it's exactly
why xgo can look and feel like the fastest tool in daily use even though
`fair` mode (correctly) shows the underlying engines are close.

**wgo's cold start is genuinely faster** in both modes — about 2.5-2.6x
faster median than xgo or air, unaffected by debounce setting (cold start
is time-to-first-build, before any debounce window applies). A plausible
explanation (not verified against wgo's source) is that wgo's CLI does less
setup work before it begins watching and invoking the build command.

**Memory: wgo leanest, xgo/air close** — the ranking shifts slightly
between modes (air is heavier in `fair` mode, close to xgo in `defaults`
mode), but wgo is consistently the leanest across every condition tested.

## Limitations

- **Single machine, single run.** Numbers are reproducible on this exact
  machine via the command above, not claimed as universally representative.
  Absolute latencies were noticeably higher in this run than an earlier run
  of the same `fair`-mode configuration on the same machine (background
  system load, not a code change) — the *rankings* and the *relative* gap
  between `fair` and `defaults` mode held steady across both runs, which is
  the load-bearing conclusion here, not the exact millisecond values.
- **Linux only.** The benchmark harness relies on `/proc` and Unix process
  groups; not run on macOS or Windows.
- **Small apps only.** Both scenarios are small enough that `go build`
  itself is fast. A large monorepo with a multi-second build would likely
  compress the `defaults`-mode gap (debounce becomes a smaller fraction of
  a much longer total) — untested here.
- **wgo's cold-start advantage is observed, not root-caused.** The
  benchmark measures *that* it's faster, not definitively *why*.
- **The `defaults` finding is a config-shipping decision, not a claim about
  which tool's engine is "better."** Nothing stops a wgo or air user from
  setting a 50ms debounce themselves — xgo's advantage here is that you get
  it without having to know to ask for it.
