# whack

[![CI](https://github.com/ItsOrganic/whack/actions/workflows/ci.yml/badge.svg)](https://github.com/ItsOrganic/whack/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Reference](https://img.shields.io/badge/go-1.25%2B-00ADD8)](go.mod)

A production-grade Go CLI hot-reloader designed as a stronger alternative to `wgo`.

## Introduction

`whack` watches your project files, coalesces noisy filesystem events, rebuilds safely, and restarts your app with minimal downtime.

Core goals:
- Fast, reliable hot-reload loops for Go development.
- Safe rebuild behavior (failed builds do not kill a healthy running process).
- Clear, colorized logs and runtime visibility.
- Cross-platform support for Linux, macOS, and Windows.

## Approach Taken

The implementation was built as a channel-driven pipeline with strict process lifecycle control:

1. Watch filesystem changes recursively with `fsnotify`.
2. Filter events using include/exclude patterns, `.gitignore`, and auto-excludes.
3. Coalesce bursts with a debouncer.
4. Fingerprint watched files (mtime + size hash) to avoid unnecessary rebuilds.
5. On real change: run hooks, build, and only replace old process if build succeeds.
6. Persist runtime status to `tmp/whack-status.yaml` for `whack status`.

Design principles used:
- Typed channels between watcher, debouncer, and runner.
- `context.Context` cancellation for clean goroutine/process shutdown.
- Minimal global state.
- Config-file defaults with CLI-flag precedence.

## Architecture (ASCII)

```text
                        +----------------------+
                        |      whack run       |
                        |  (cobra command)     |
                        +----------+-----------+
                                   |
                                   v
                    +-------------------------------+
                    |      internal/config          |
                    | load whack.yaml + merge flags |
                    +---------------+---------------+
                                    |
                                    v
      fsnotify events      +--------+---------+       BuildSignal
+-------------------------> | internal/watcher | ------------------+
|                           | recursive watch   |                  |
|                           | gitignore/pattern |                  v
|                           +-------------------+         +-------------------+
|                                                             internal/debouncer|
|                                                             coalescing + hash |
|                                                             fingerprint check  |
|                                                            +---------+---------+
|                                                                      |
|                                                                      v
|                                                            +-------------------+
|                                                            | internal/runner   |
|                                                            | build/kill/start  |
|                                                            | hooks + extra cmds|
|                                                            +---------+---------+
|                                                                      |
|                                                                      v
|                                                            +-------------------+
|                                                            | app process(es)   |
|                                                            | + extra commands  |
|                                                            +-------------------+
|
+--> status snapshots --> tmp/whack-status.yaml --> `whack status`
```

## Project Structure

```text
whack/
├── main.go
├── go.mod
├── go.sum
├── README.md
├── cmd/
│   ├── root.go
│   ├── run.go
│   ├── init.go
│   ├── status.go
│   └── runtime_state.go
├── internal/
│   ├── watcher/watcher.go
│   ├── debouncer/debouncer.go
│   ├── runner/
│   │   ├── runner.go
│   │   ├── proc_unix.go
│   │   └── proc_windows.go
│   ├── config/config.go
│   └── logger/logger.go
└── whack.yaml.example
```

## Install

```bash
go install github.com/ItsOrganic/whack@latest
```

Or download a prebuilt binary from [Releases](https://github.com/ItsOrganic/whack/releases).

### Homebrew

```bash
brew install ItsOrganic/tap/whack
```

### Initialize config

```bash
whack init
```

This creates `whack.yaml` in your current directory.

## Usage

### Start hot-reload

```bash
whack run
```

### Build a specific file or package path

Use a positional build target when your main package is not at project root:

```bash
whack run src/main.go
```

If the first positional argument is an existing `.go` file or directory, `whack` treats it as the build target (unless `--build` is explicitly set).

### Override settings from CLI

```bash
whack run \
  --watch ./cmd --watch ./internal \
  --include "*.go" --include "*.html" \
  --exclude "*_test.go" \
  --debounce 100ms \
  --build "go build -o ./tmp/whack-app ." \
  --cmd "./tmp/whack-app" \
  --before "go generate ./..." \
  --after "echo restarted" \
  --extra-cmd "npm run watch" \
  --timestamps
```

### Show runtime status

```bash
whack status
```

Reports `RUNNING (pid=...)` or `NOT RUNNING (stale - last updated ...)` based
on a real liveness check against the recorded PID, not just the presence of
a status file.

### Diagnose a broken setup

```bash
whack doctor
```

Validates your config, checks that at least one directory is actually being
watched, and sanity-checks that your build/run commands look runnable —
before you spend time wondering why saves aren't triggering rebuilds.

## Default Behavior Highlights

- Watches `.` recursively by default.
- Includes `*.go` by default.
- Debounce delay defaults to `50ms`.
- Build timeout defaults to `30s`.
- Auto-excludes: `vendor/`, `.git/`, `tmp/`, `node_modules/`, `*.pb.go`, `*_mock.go`, output binary.
- Build failure keeps old healthy process running.
- The generated build command passes `-ldflags="-s -w"`, dropping the symbol
  table and DWARF info the reload loop never reads. That's ~20% off every
  build and a ~30% smaller binary — one-off micro-benchmarks on the
  maintainer's machine, not part of the reproducible suite in
  [`benchmark/`](benchmark/), so treat them as indicative. Drop the flags
  from `build.cmd` in your own `whack.yaml` if you need to attach a debugger.
- `chmod`-only changes don't trigger rebuilds — they can't affect the build,
  and `go build` itself emits them.

## Notes

- `tmp/` is auto-added to `.gitignore` if missing.
- Runtime status is stored in `tmp/whack-status.yaml`.
- The compiled binary is left in `tmp/` on exit, on purpose. `go build -o X`
  only takes its up-to-date fast path when `X` already exists, so deleting
  it would cost the next `whack run` a full re-link — measured at 630ms vs
  234ms to first ready in a one-off A/B on an older Go toolchain, not part
  of the reproducible suite in [`benchmark/`](benchmark/). On go1.25.13 the
  reproducible cold-start figure is ~62-66ms; see
  [BENCHMARKS.md](BENCHMARKS.md). `tmp/` is gitignored, so nothing leaks
  into the repo.

## Inspiration

`whack` is inspired by [`wgo` (watcher-go)](https://github.com/bokwoon95/wgo), an open-source live reload tool for Go apps and general commands.

## whack vs wgo

Both tools solve hot reload, but they optimize for different workflows:

| Area | whack | wgo |
| --- | --- | --- |
| Workflow style | Config-first (`whack.yaml`) plus CLI overrides | CLI-first, command-chain oriented |
| Process safety | Keeps healthy process running if rebuild fails | General-purpose rerun model |
| Change handling | Debounce + fingerprint checks to reduce duplicate rebuilds | Event-driven reruns with filtering flags |
| Runtime visibility | Built-in `whack status` with `tmp/whack-status.yaml` snapshots | No equivalent persisted runtime status command |
| Hooks and side tasks | `before`/`after` hooks + `extra_cmds` in config | Chaining/parallel commands via CLI separators |
| Defaults | Auto-excludes common noisy dirs/files; ships a 50ms debounce and a stripped build command | Minimal, generic watcher defaults |

If you prefer a small, CLI-native watcher, `wgo` is excellent. If you want a structured, project-configured hot-reload loop with stronger lifecycle controls, `whack` is built for that.

## Development

```bash
go build -o whack .
go test ./... -race
gofmt -l .
go vet ./...
```

## Benchmarks

whack vs `wgo` vs `air` on rebuild latency, cold-start latency, and resource
usage — see [BENCHMARKS.md](BENCHMARKS.md).

## Contributing

Contributions are welcome. Fork, branch, and open a PR — `go build`,
`go test ./... -race`, `gofmt -l .`, and `go vet ./...` should all be clean
before you do.

## License

MIT — see [LICENSE](LICENSE).

## Thanks

Created by itsorganic
