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
  rebuild latency is **~2.0x faster than wgo** and **~4.4x faster than
  air**, consistently across both scenarios.
- **Cold start ties with wgo** (~66ms vs ~64ms) and is **~4.4x faster than
  air** (~295ms). air pays a full re-link on every start because it deletes
  its own build output on exit.
- **`fair` mode: near-parity, with xgo trailing slightly.** Held to the same
  debounce and the same build command, xgo is consistently **~17ms (~3.4%)
  slower** than wgo. That gap is small but it points the same direction in
  every measurement, so it is reported as a real if minor deficit rather
  than a tie — it is xgo's own per-cycle overhead, and it is the thing
  worth attacking next.
- **wgo remains the leanest on memory** (~36-45 MiB vs xgo's ~46-51 MiB).
- Zero failed trials across all 336 measured runs.

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
   400ms apart, timing edit-to-ready for each. Discard the first 3 as
   warmup, report median/p95 of the remaining 17.
3. **Resource usage**: sampled every 200ms during the rebuild-latency loop,
   walking `/proc` for the tool's *entire* process tree. Peak RSS is
   dominated by the `go build` compiler subprocess.

**Reproduce it**:
```bash
cd benchmark
go run . --results results.json
```

## Results — defaults mode (each tool exactly as it ships)

### Minimal scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **296.7** | 596.4 | 1297.4 |
| Rebuild latency p95 (ms) | **328.6** | 634.3 | 1335.4 |
| Cold-start median (ms) | 66.3 | **62.8** | 292.1 |
| Avg memory (MiB) | 46.0 | **39.9** | 44.8 |
| Avg CPU during rebuild loop | 41.5% | 39.7% | 25.6% |

### Realistic scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | **299.7** | 588.4 | 1304.3 |
| Rebuild latency p95 (ms) | **328.1** | 601.5 | 1326.6 |
| Cold-start median (ms) | **65.5** | 65.9 | 296.7 |
| Avg memory (MiB) | 46.1 | **36.2** | 45.2 |
| Avg CPU during rebuild loop | 42.1% | 38.3% | 23.8% |

Two shipped defaults drive xgo's rebuild win: a 50ms debounce against wgo's
300ms and air's 1000ms, and `-ldflags="-s -w"`, which drops the symbol table
and DWARF info the reload loop never reads. Linking is the most expensive
phase of a rebuild, and skipping that output measures at **78ms saved per
build** in isolation on this toolchain (386ms → 308ms).

air's lower CPU% is a direct consequence of its 1000ms delay — it is doing
less work per second because it reacts a full second later, not because it
is more efficient per rebuild. See [A note on the CPU metric](#a-note-on-the-cpu-metric).

## Results — fair mode (equal debounce, identical build command)

### Minimal scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 513.6 | **496.6** | 496.7 |
| Rebuild latency p95 (ms) | 529.8 | **507.1** | 509.1 |
| Cold-start median (ms) | 65.6 | **64.1** | 297.5 |
| Avg memory (MiB) | 50.7 | **39.0** | 58.4 |
| Avg CPU during rebuild loop | 42.5% | 45.5% | 45.0% |

### Realistic scenario

| Metric | xgo | wgo | air |
|---|---:|---:|---:|
| Rebuild latency median (ms) | 515.5 | 497.9 | **492.8** |
| Rebuild latency p95 (ms) | 534.5 | 530.0 | **514.1** |
| Cold-start median (ms) | 64.7 | **63.8** | 297.5 |
| Avg memory (MiB) | 51.1 | **44.9** | 57.8 |
| Avg CPU during rebuild loop | 44.7% | 49.0% | 44.9% |

**xgo trails by ~17ms here, and the direction is consistent.** Across every
fair-mode measurement taken — both scenarios on this toolchain, and both
scenarios on the previous one — xgo is the slowest of the three, by 13-20ms.
An earlier version of this report called that a statistical tie; with the
gap pointing the same way in all four comparisons, it is more honest to call
it a small real deficit. It is xgo's own per-cycle overhead — plausibly the
`sh -c` wrapper around both build and app, the extra event-relay hop, and
fingerprint recomputation — and it is the natural next target.

## Interpretation

**Rebuild latency is dominated by `go build`.** Component costs measured
directly on this machine and toolchain:

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
and why the only large levers are linking less (P1's stripping flags) or
not building at all.

**Cold start is about the artifact lifecycle.** `go build -o X` only takes
its fast path when `X` already exists; otherwise it links from scratch at
~6.6x the cost. xgo and air both used to delete their build output on exit
(air still does, via `clean_on_exit`), paying a full re-link on every start.
That is the entire explanation for air's ~295ms against xgo's and wgo's
~65ms. It also shows up inside each tool's own trial series: trial 1 costs
~200-283ms for xgo and wgo (nothing built yet), then every subsequent trial
drops to ~65ms — while air stays at ~290ms for all eight.

**Memory: wgo leanest, consistently.** wgo's supervisor process idles at
~4.1 MiB against xgo's ~10.9 MiB and air's ~15.9 MiB. Reducing xgo's
supervisor footprint is open work.

## Toolchain note (read before comparing to your own shell)

These numbers were produced with `go` resolving **directly** to the
go1.25.13 toolchain. That matters more than it sounds: the harness is
launched via `go run .`, and the go command prepends its resolved
toolchain's `bin` to `PATH` for child processes. So every tool it launches
gets a `go` that needs no toolchain re-exec.

If your `PATH` `go` is an older release than your `go.mod` requires, every
build additionally pays a re-exec into the newer toolchain. On this machine
that costs about **100ms per build** (a fast-path build measures 58ms when
invoked directly against the 1.25.13 toolchain, versus ~155ms through a
1.21.13 `go` that has to re-exec).

This does not bias the comparison — all three tools are launched identically
by the same harness and get the same environment — but it does mean the
absolute latencies here are a floor rather than what you will see in a shell
whose primary `go` is older. Installing the toolchain your `go.mod` targets
as your primary `go` is worth more per build than any config flag in this
report.

## What changed since the previous run

Two changes, in order.

**xgo stopped deleting its build output on exit.** The previous report
showed xgo cold-starting at ~630ms against wgo's ~240ms and flagged the
cause as "observed, not root-caused." Root cause: xgo deleted `tmp/xgo-app`
on exit, so every run paid a full re-link. Removing that moved cold start
from 630ms to 234ms in a controlled A/B on the same toolchain.

**The project moved from Go 1.23.0 to go1.25.13.** That is worth its own
comparison, since the compiler is ~95% of every measured cycle. Rebuild
latency, median ms, same machine, same code:

| | 1.23.0 | 1.25.13 | change |
|---|---:|---:|---:|
| xgo, defaults, minimal | 340.0 | 296.7 | −12.8% |
| xgo, defaults, realistic | 342.9 | 299.7 | −12.6% |
| wgo, defaults, minimal | 628.8 | 596.4 | −5.1% |
| air, defaults, minimal | 1322.7 | 1297.4 | −1.9% |
| xgo, fair, minimal | 562.8 | 513.6 | −8.7% |
| wgo, fair, minimal | 549.7 | 496.6 | −9.7% |
| air, fair, minimal | 559.6 | 496.7 | −11.2% |

Cold start improved far more dramatically for the two tools that keep their
binary — xgo 150→66ms and wgo 149→63ms, roughly 2.3x — because Go 1.25's
up-to-date fast path got much cheaper (265ms → 58ms). air, which re-links
every start, improved only 330→292ms. The compiler upgrade therefore widened
xgo's `defaults`-mode lead: 1.85x → 2.0x over wgo, and 3.9x → 4.4x over air.

Compare within a run, never across runs — background load moves absolute
numbers substantially, which is why the whole table is regenerated rather
than having individual rows updated.

## A note on the CPU metric

Average CPU% structurally penalizes whichever tool is fastest: the same
compiler work compressed into a shorter window reads as a higher percentage.
air's 25.6% is not efficiency, it is a 1000ms delay. **Total CPU-seconds per
edit-to-ready cycle** would be the honest metric, and the harness already
reads cumulative ticks from `/proc`, so it is a subtraction rather than a
rate. Treat the CPU column as directional only.

## Limitations

- **Single machine, single run.** Reproducible on this exact machine via the
  command above, not claimed as universally representative.
- **Linux only.** The harness relies on `/proc` and Unix process groups.
- **Small apps only.** Both scenarios are small enough that `go build` is
  fast. A large monorepo with a multi-second build would compress the
  `defaults`-mode gap, since debounce becomes a smaller fraction of a much
  longer total — untested here.
- **The `defaults` finding is a config-shipping decision**, not a claim
  about which engine is better. `fair` mode is the architecture comparison,
  and there xgo is marginally *behind*.
- **A known bug affected data collection.** xgo intermittently leaks its app
  process on shutdown (orphaned, still holding its port), causing
  `bind: address already in use` on subsequent trials. Affected combinations
  were re-run after clearing strays, and every number above comes from a
  clean run with zero failures — but the bug is real and unfixed.
