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
[`benchmark/`](benchmark/) — every number in the results tables below is
reproducible with one command. A handful of supporting figures come from
separate one-off micro-benchmarks the harness does not emit; each is tagged
inline as *(micro-benchmark, not in `results.json`)* so you can tell at a
glance which numbers you can reproduce and which you have to take on trust.

## TL;DR

- **`defaults` mode: with default settings, xgo rebuilds substantially
  faster.** Out of the box, median rebuild latency is **~2x faster than
  wgo** and **~4.4x faster than air**, consistently across both scenarios.
- **Cold start is level with wgo** (~66ms vs ~61ms — xgo is a few
  milliseconds *behind* in every measurement) and, **with default settings,
  ~4.3x faster than air** (~272-284ms — the gap holds in `fair` mode too).
  air pays a full re-link on every start because it deletes
  its own build output on exit.
- **`fair` mode: xgo loses.** Held to the same debounce and the same build
  command, xgo is the slowest of the three in both scenarios — 10-23ms
  (2-5%) behind wgo and air. That is xgo's own per-cycle overhead, and it
  is the thing worth attacking next.
- **wgo is the leanest on memory, and xgo is not.** xgo averages more
  memory than wgo in every configuration measured: 42.9 vs 40.6 MiB in the
  minimal `defaults` case (+6%) and 47.1 vs 37.5 MiB in the realistic one
  (+26%).
- **On CPU, read two numbers together.** air posts the lowest average CPU%
  in `defaults` mode (21.6%), which is its 1000ms delay, not efficiency.
  xgo's 38.1% and wgo's 40.3% are close enough to be noise — in the
  realistic scenario the gap is 0.3 percentage points. The metric that
  actually separates them is **CPU-seconds per rebuild**, where xgo's
  faster cycle costs less total work: 0.101 vs wgo's 0.233 and air's 0.278
  (minimal, `defaults`). See [A note on the CPU metric](#a-note-on-the-cpu-metric).
- Zero failed trials across all 336 measured runs.

### Run-to-run variance (read this before quoting any ratio)

Two runs of **identical code on this same machine** produced xgo
`defaults`/minimal medians of **296.7ms** and **265.0ms** — a 31.7ms spread,
roughly **11%** of the larger figure. The most plausible cause is build-cache
warmth, not a code change. Everything in the tables below comes from the
second of those runs, and the raw data for it ships in
[`benchmark/results.json`](benchmark/results.json).

Because of that spread, the headline ratios in this document are stated
conservatively — **~2x vs wgo, ~4.4x vs air** — which is the *weaker* of the
two runs of the same code. The tables report exactly what was measured; the
prose rounds down. Treat every ratio here as approximate.

## Methodology

**Machine**: AMD Ryzen 5 5600H (12 threads), 13GiB RAM, Kali GNU/Linux
Rolling (Linux 6.6.15), linux/amd64.

**Go toolchain**: **go1.25.13**, pinned via the `toolchain` directive in
both `go.mod` files. See [Toolchain note](#toolchain-note-read-before-comparing-to-your-own-shell)
— it materially affects absolute numbers.

**Tool versions**: `xgo` built from this repo's current source
(`benchmark/` builds it fresh on every run, so it always measures the
committed code, never a stale local install). `wgo` v0.6.4. `air` v1.67.4.

**Test apps** (`benchmark/testapps/`):
- **minimal**: one file, one dependency-free HTTP server. Isolates each
  tool's own watch/restart overhead from Go compiler speed.
- **realistic**: ~5 packages (an LRU cache, string utils, math utils, and a
  service layer combining them), a few hundred LOC.

Both apps print `READY build=<N> ts=<unixnano>` the instant they're bound
and about to accept connections. This is the only thing the harness watches
for on each tool's stdout — detection logic is identical regardless of which
tool is wrapping the process.

**The two modes, concretely.** `fair` mode gives all three tools a 200ms
debounce/delay *and the identical build command* `go build -o ./tmp/X-app .`.
`defaults` mode omits the debounce key so each tool falls back to its own
built-in (xgo 50ms, wgo 300ms, air 1000ms), and gives xgo the build
command it actually ships (`config.DefaultBuildCmd`, which builds
`./tmp/xgo-app` and passes `-ldflags="-s -w"`). Those stripping flags are a
shipped default in exactly the same sense as the debounce value — nothing
stops a wgo or air user from passing them too — so they belong in `defaults`
mode and are deliberately kept out of `fair` mode. The exact configs used are
in [`benchmark/configs/`](benchmark/configs/)
(`xgo-minimal.yaml`, `xgo-minimal-defaults.yaml`,
`xgo-realistic.yaml`, `xgo-realistic-defaults.yaml`, and the wgo/air
equivalents).

**Measurements**, per (tool, scenario, mode) triple — 12 triples in all:
1. **Cold-start latency**: 8 full launch→first-ready cycles, discard the
   first as warmup, report median/p95 of the remaining 7.
2. **Rebuild latency**: start the tool once, confirm the first build comes
   up (not counted — that's cold-start), then make 20 simulated edits
   400ms apart, timing edit-to-ready for each. Discard the first 3 as
   warmup, report median/p95 of the remaining 17.
3. **Resource usage**: sampled every 200ms during the rebuild-latency loop,
   walking `/proc` for the tool's *entire* process tree. Peak RSS is
   dominated by the `go build` compiler subprocess.
4. **CPU-seconds per rebuild**: derived, not sampled separately — median
   rebuild latency × average CPU% over the rebuild loop.

**Reproduce it**:
```bash
cd benchmark
go run . --results results.json
```

## Results — defaults mode (each tool exactly as it ships)

### Minimal scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **265.0** | 579.1 | 1285.1 |
| Rebuild latency p95 (ms) | **291.4** | 595.4 | 1321.5 |
| Cold-start median (ms) | 65.9 | **61.2** | 284.4 |
| Avg memory (MiB) | 42.9 | **40.6** | 44.1 |
| Avg CPU during rebuild loop | 38.1% | 40.3% | 21.6% |
| CPU-seconds per rebuild | **0.101** | 0.233 | 0.278 |

### Realistic scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **247.7** | 572.5 | 1270.8 |
| Rebuild latency p95 (ms) | **257.3** | 598.3 | 1286.0 |
| Cold-start median (ms) | 61.3 | **60.3** | 272.3 |
| Avg memory (MiB) | 47.1 | **37.5** | 44.5 |
| Avg CPU during rebuild loop | 35.7% | 36.0% | 21.9% |
| CPU-seconds per rebuild | **0.089** | 0.206 | 0.279 |

Two shipped defaults drive xgo's rebuild advantage **with default
settings**: a 50ms debounce against wgo's 300ms and air's 1000ms, and
`-ldflags="-s -w"`, which drops the symbol table and DWARF info the reload
loop never reads. Linking is the most expensive phase of a rebuild, and
skipping that output measures at **78ms saved per build** in isolation on
this toolchain (386ms → 308ms).

Note what the memory column says: xgo is **not** the lean option here. wgo
averages less memory in both scenarios, and the gap widens to +26% in the
realistic one.

air's lower CPU% is a direct consequence of its 1000ms delay — it is doing
less work per second because it reacts a full second later, not because it
is more efficient per rebuild. xgo's average CPU% is within noise of wgo's
(0.3 percentage points apart in the realistic scenario); the CPU-seconds row
is the one that reflects real work done. See
[A note on the CPU metric](#a-note-on-the-cpu-metric).

## Results — fair mode (equal debounce, identical build command)

**This is the section where xgo loses.** With the shipped configuration
differences removed, xgo is the slowest of the three tools in both
scenarios.

### Minimal scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 493.6 | **480.6** | 483.3 |
| Rebuild latency p95 (ms) | 542.8 | **494.7** | 503.8 |
| Cold-start median (ms) | 65.9 | **63.4** | 282.9 |
| Avg memory (MiB) | 53.6 | **44.6** | 58.7 |
| Avg CPU during rebuild loop | 42.3% | 45.6% | 46.0% |
| CPU-seconds per rebuild | **0.209** | 0.219 | 0.222 |

### Realistic scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 493.6 | 473.7 | **470.3** |
| Rebuild latency p95 (ms) | 503.4 | 509.4 | **499.6** |
| Cold-start median (ms) | 66.0 | **59.8** | 270.6 |
| Avg memory (MiB) | 50.4 | **41.8** | 55.2 |
| Avg CPU during rebuild loop | 42.6% | 44.1% | 37.1% |
| CPU-seconds per rebuild | 0.210 | 0.209 | **0.175** |

**xgo trails in every fair-mode comparison, and the direction is
consistent.** It is 13.1ms (+2.7%) behind wgo and 10.3ms (+2.1%) behind air
in the minimal scenario, and 19.9ms (+4.2%) behind wgo and 23.3ms (+5.0%)
behind air in the realistic one. An earlier version of this report called
that a statistical tie; with the gap pointing the same way in all four
comparisons, it is more honest to call it a small real deficit. It is
xgo's own per-cycle overhead — plausibly the `sh -c` wrapper around both
build and app, the extra event-relay hop, and fingerprint recomputation —
and it is the natural next target.

The CPU-seconds row tells the same story. Once the debounce advantage is
removed, xgo's CPU-seconds per rebuild is level with wgo's (0.209 vs
0.219 minimal, 0.210 vs 0.209 realistic) and **worse than air's** in the
realistic scenario (0.210 vs 0.175). The `defaults`-mode CPU-seconds
advantage comes from finishing the cycle sooner, not from a cheaper cycle.

## Interpretation

**Rebuild latency is dominated by `go build`.** Component costs measured
directly on this machine and toolchain (these are separate micro-benchmarks,
not part of `results.json`):

| Phase | Cost |
|---|---:|
| xgo's own startup (config, watcher walk, fingerprint priming) | ~10 ms |
| stop old process + fork/exec new one | ~10 ms |
| launching an already-built binary to READY | ~3 ms |
| `go build`, output present and unchanged (fast path) | 58 ms |
| `go build`, output absent (full link) | 386 ms |
| `go build -ldflags="-s -w"`, output absent | 308 ms |

A rebuild always relinks — the source changed, so the fast path cannot
apply — which is why rebuild latency sits near the link cost plus debounce,
and why the only large levers are linking less (the stripping flags) or
not building at all.

**Cold start is about the artifact lifecycle.** `go build -o X` only takes
its fast path when `X` already exists; otherwise it links from scratch at
~6.6x the cost. xgo and air both used to delete their build output on exit
(air still does, via `clean_on_exit`), paying a full re-link on every start.
That is the entire explanation for air's ~272-284ms against xgo's and
wgo's ~60-66ms. It also shows up inside each tool's own trial series: trial
1 costs ~195-316ms for xgo and wgo (nothing built yet), then subsequent
trials drop to 55.5-70.2ms — 52 of those 56 samples, with four outliers at
83.7, 90.3, 101.5 and 105.9ms — while air stays at ~260-355ms for all eight.

**Memory: wgo is leanest, consistently; xgo is not.** wgo averages the
least memory in all four configurations (37.5-44.6 MiB) against xgo's
42.9-53.6 MiB and air's 44.1-58.7 MiB — those four ranges are from the
tables above, so they are reproducible.

The cause is a separate measurement: the supervisor process alone idles at
~4.1 MiB for wgo against xgo's ~10.9 MiB and air's ~15.9 MiB *(these three
figures are a one-off micro-benchmark, not in `results.json` — the harness
reports whole-process-tree averages, not per-supervisor idle RSS)*. Reducing
xgo's supervisor footprint is open work.

## Toolchain note (read before comparing to your own shell)

These numbers were produced with `go` resolving **directly** to the
go1.25.13 toolchain. That matters more than it sounds: the harness is
launched via `go run .`, and the go command prepends its resolved
toolchain's `bin` to `PATH` for child processes. So every tool it launches
gets a `go` that needs no toolchain re-exec.

If your `PATH` `go` is an older release than your `go.mod` requires, every
build additionally pays a re-exec into the newer toolchain. On this machine
that costs about **100ms per build** (a fast-path build measures 58ms when
invoked directly against the go1.25.13 toolchain, versus ~155ms through a
1.21.13 `go` that has to re-exec). *These three figures are a one-off
micro-benchmark, not in `results.json`: the harness always runs against a
directly-resolved toolchain, so it never measures the re-exec path.*

This does not bias the comparison — all three tools are launched identically
by the same harness and get the same environment — but it does mean the
absolute latencies here are a floor rather than what you will see in a shell
whose primary `go` is older. Installing the toolchain your `go.mod` targets
as your primary `go` is worth more per build than any config flag in this
report.

## What changed since the previous run

Two changes, in order.

**xgo stopped deleting its build output on exit.** An earlier report
showed the tool cold-starting at ~630ms against wgo's ~240ms and flagged the
cause as "observed, not root-caused." Root cause: it deleted `tmp/xgo-app`
on exit, so every run paid a full re-link. Removing that moved cold start
from 630ms to 234ms in a controlled A/B on the same toolchain, and it now
sits at ~62-66ms on go1.25.13.

**The project moved from Go 1.23.0 to go1.25.13.** That is worth its own
comparison, since the compiler is ~95% of every measured cycle. Rebuild
latency, median ms, same machine, same code:

| | 1.23.0 | go1.25.13 | change |
|---|---:|---:|---:|
| xgo, defaults, minimal | 340.0 | 265.0 | −22.1% |
| xgo, defaults, realistic | 342.9 | 247.7 | −27.7% |
| wgo, defaults, minimal | 628.8 | 579.1 | −7.9% |
| air, defaults, minimal | 1322.7 | 1285.1 | −2.8% |
| xgo, fair, minimal | 562.8 | 493.6 | −12.3% |
| wgo, fair, minimal | 549.7 | 480.6 | −12.6% |
| air, fair, minimal | 559.6 | 483.3 | −13.6% |

The `1.23.0` column is a historical run that is not shipped in
`results.json`; the `go1.25.13` column is the current run that is. Because
those are two different runs, the change column carries the full
run-to-run spread described in
[Run-to-run variance](#run-to-run-variance-read-this-before-quoting-any-ratio)
— read the direction, not the third significant figure.

Cold start improved far more dramatically for the two tools that keep their
binary — xgo 150→66ms and wgo 149→61ms, roughly 2.3x — because Go 1.25's
up-to-date fast path got much cheaper (265ms → 58ms — a micro-benchmark of
`go build` alone on each toolchain, not in `results.json`). air, which re-links
every start, improved only 330→284ms. The compiler upgrade therefore widened
xgo's `defaults`-mode lead: 1.85x → ~2x over wgo, and 3.9x → ~4.4x over
air.

Compare within a run, never across runs — background load and build-cache
warmth move absolute numbers substantially, which is why the whole table is
regenerated rather than having individual rows updated.

## A note on the CPU metric

Average CPU% structurally penalizes whichever tool is fastest: the same
compiler work compressed into a shorter window reads as a higher percentage.
air's 21.6% in `defaults` mode is not efficiency, it is a 1000ms delay.
**Total CPU-seconds per edit-to-ready cycle** is the honest metric, and it
is reported alongside the percentage in every table above — computed as
median rebuild latency × average CPU%.

Two consequences worth stating plainly:

- The percentage column should never be read as a ranking. xgo's 38.1%
  against wgo's 40.3% (and 35.7% against 36.0% in the realistic scenario) is
  well inside run-to-run noise; it is not an advantage and is not claimed as
  one.
- The CPU-seconds advantage in `defaults` mode is a consequence of the
  shorter cycle, not of a cheaper cycle. In `fair` mode it disappears —
  xgo sits level with wgo and behind air on CPU-seconds in the realistic
  scenario.

The derived CPU-seconds figure inherits the sampling error of the 200ms
resource sampler, so treat differences under ~10% as inconclusive.

## Limitations

- **Single machine, single run.** Reproducible on this exact machine via the
  command above, not claimed as universally representative. See
  [Run-to-run variance](#run-to-run-variance-read-this-before-quoting-any-ratio)
  for how much two identical runs actually differed.
- **Linux only.** The harness relies on `/proc` and Unix process groups.
- **Small apps only.** Both scenarios are small enough that `go build` is
  fast. A large monorepo with a multi-second build would compress the
  `defaults`-mode gap, since debounce becomes a smaller fraction of a much
  longer total — untested here.
- **The `defaults` finding is a config-shipping decision**, not a claim
  about which engine is better. `fair` mode is the architecture comparison,
  and there xgo is *behind* in both scenarios.
- **xgo is not the lightest tool.** It uses more memory than wgo in every
  configuration measured, by 6-26%.
- **A known bug affected data collection.** xgo intermittently leaks its
  app process on shutdown (orphaned, still holding its port), causing
  `bind: address already in use` on subsequent trials. Affected combinations
  were re-run after clearing strays, and every number above comes from a
  clean run with zero failures — but the bug is real and unfixed.
