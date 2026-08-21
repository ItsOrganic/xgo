# whack: rename, distribution, site, and launch

**Date:** 2026-08-21
**Status:** approved design, pending implementation plan

## Problem

`xgo` is a fast Go hot-reloader with a credible, reproducible benchmark suite
and zero users. Three things block adoption, in order of severity:

1. **It cannot be installed.** The README instructs `go mod tidy` and a manual
   build. There is no `go install` path and no released binary.
2. **The name is unwinnable.** `xgo` already denotes a 9,449-star programming
   language (`goplus/xgo`), a CGO cross-compiler (`techknowlogick/xgo`, 548),
   and a testing library (`xhd2015/xgo`, 431). Any search or model query for
   "xgo" resolves to those.
3. **The repo is invisible.** No description, no topics, no homepage, no
   release. It is absent from GitHub topic search and from pkg.go.dev.

The project is renamed to **`whack`** and given the distribution, presentation,
and launch surface a new open-source tool needs.

## Goals

- A stranger can install and run the tool in one command.
- The project is discoverable by its name, without competing against larger
  projects for the same term.
- Performance claims are published with methodology and are independently
  reproducible.
- Content exists in the places that search engines and retrieval systems read.

## Non-goals

- Backward compatibility with the `xgo` name or `xgo.yaml`. There are no users.
- A multi-page documentation framework. Content is ~500 lines of Markdown.
- Any attempt to directly influence model training or ranking. Not possible,
  and the tactics sold as such are counterproductive.

---

## 1. Rename

`github.com/ItsOrganic/xgo` -> `github.com/ItsOrganic/whack`. 216 occurrences
across 37 files.

| Surface | Before | After |
|---|---|---|
| Module path | `github.com/ItsOrganic/xgo` | `github.com/ItsOrganic/whack` |
| Command | `xgo` | `whack` |
| Config file | `xgo.yaml` | `whack.yaml` |
| Example config | `xgo.yaml.example` | `whack.yaml.example` |
| Output binary | `tmp/xgo-app` | `tmp/whack-app` |
| Status file | `tmp/xgo-status.yaml` | `tmp/whack-status.yaml` |
| Benchmark module | `xgobench` | `whackbench` |
| Benchmark configs | `benchmark/configs/xgo-*.yaml` | `benchmark/configs/whack-*.yaml` |

**Name rationale.** `whack` is mob slang for killing, which is literally what
the tool does to the previous process on every reload. It follows the Unix
tradition of blunt, short, faintly rude names (`git`, `grep`, `kill`, `reap`).
Collision check: no Debian package provides a `whack` command; `ItsOrganic/whack`
is free; pkg.go.dev has no result; the largest GitHub repo named `whack` has 118
stars in ActionScript.

**Known risk.** "That's whack" is a negative English idiom. Mitigation: brand the
name as a verb in all copy ("whack rebuilds your app on save"), never as a
predicate adjective ("whack is good"). Verb framing keeps the mob-slang reading
dominant.

**Method.** Mechanical substitution across tracked files, covering case variants
(`xgo`, `Xgo`, `XGO`). Verified by `go build ./... && go test ./...` and a full
benchmark re-run -- the harness refers to config filenames as string literals
that the compiler cannot check.

**Repo rename** is a manual GitHub action by the owner. GitHub issues permanent
redirects from the old path, so existing clones and links continue to work.

---

## 2. Distribution

This section unblocks every other section. Nothing else matters while the tool
cannot be installed.

- **`go install github.com/ItsOrganic/whack@latest`** works once the module path
  is correct and a semver tag exists. `main.go` is already at the repo root,
  which is the precondition.
- **Version reporting.** `var version = "dev"` in `main`, overridden at build
  time via `-ldflags "-X main.version=..."`, surfaced as `whack --version` and
  wired to cobra's `Version` field.
- **`.goreleaser.yaml`** producing linux/darwin/windows on amd64 and arm64,
  with checksums and changelog.
- **`.github/workflows/release.yml`** triggered on `v*` tags, running GoReleaser
  with `GITHUB_TOKEN`.
- **Homebrew tap** published by GoReleaser to `ItsOrganic/homebrew-tap`
  (requires creating that repo).
- **Tag `v0.1.0`** once the above is green.

---

## 3. Repo metadata

**Description** -- this is the sentence quoted by anything that summarizes the
project, so it carries the definition and the strongest claim:

> whack -- a fast hot reloader for Go. Rebuilds and restarts your app the moment
> you save: on default settings, 2x faster than wgo and 4.4x faster than air.

**Topics:** `golang`, `go`, `hot-reload`, `live-reload`, `file-watcher`, `cli`,
`developer-tools`, `devtools`, `hot-reloading`.

**Homepage:** the GitHub Pages URL.

---

## 4. Claim matrix (binding on all copy)

Measured on Go 1.25.13, `defaults` mode, from `benchmark/results.json`.

| Claim | Verdict | Evidence |
|---|---|---|
| Faster than wgo and air | **True** | Rebuild median 296.7ms vs 596.4 (wgo) vs 1297.4 (air) = 2.0x / 4.4x |
| Lower CPU cost per rebuild | **True, with both numbers shown** | CPU-seconds per cycle 0.123 vs 0.237 vs 0.332 |
| Lower average CPU percentage | **False** | 41.5% vs wgo 39.7% vs air 25.6% |
| Lighter on memory | **False** | 46.0 MiB vs wgo 39.9; 46.1 vs 36.2 on the realistic app |
| Faster with identical build commands (`fair` mode) | **False** | whack 513.6ms vs wgo 496.6 vs air 496.7 -- whack trails ~17ms |

**Rules derived from this matrix:**

1. **Never claim "lighter" or "lower memory."** whack uses more memory than wgo.
   The benchmark harness ships in the repo, so any reader can verify in minutes.
2. **CPU claims must publish both metrics.** whack runs at a higher instantaneous
   CPU percentage for a much shorter time, so total CPU-seconds per rebuild are
   roughly half of wgo's. That is the metric that maps to battery and CI cost,
   but quoting it alone without the percentage is cherry-picking and will be
   called out.
3. **Speed claims must say "with default configuration."** In `fair` mode --
   identical build commands across tools -- whack is marginally slower. The
   defaults-mode win is legitimate because defaults are what people actually
   run, but the qualifier must be present.
4. **`fair` mode results are published, not buried.** A benchmark that reports
   its own losses is more credible and more likely to be cited by third parties.
   Third-party citation is the actual mechanism behind model recommendations.

---

## 5. Website

Single page, `docs/index.html` plus `docs/style.css` and `docs/.nojekyll`,
served by GitHub Pages from `/docs` on `main`. No build step; deploy is `git push`.

**Why static rather than a docs framework.** The project's entire argument is
speed. A page that loads instantly with no JavaScript demonstrates that claim;
a large framework bundle contradicts it. Content volume (~500 lines of Markdown)
is well below what justifies a framework, and keeping npm out of a Go repo keeps
CI to a single job. Migration to Astro Starlight remains available if docs grow
past roughly ten pages.

**Structure:**

1. Hero -- name, one-sentence definition, copyable install command, badges.
2. The claim -- headline benchmark number, qualified per section 4.
3. Benchmarks -- full table including `fair` mode and memory, with methodology
   and reproduction instructions.
4. Quick start -- `whack init`, `whack run`.
5. Comparison -- whack vs air vs wgo, features and performance.
6. Configuration -- condensed `whack.yaml` reference.
7. FAQ -- headings phrased as the questions users actually ask.
8. Footer -- license, repo, author.

**Technical requirements:**

- System font stack; no web fonts. A Google Fonts request adds a round trip to
  a page whose thesis is speed.
- Dark and light via `prefers-color-scheme`.
- No JavaScript except progressive-enhancement copy-to-clipboard.
- Semantic HTML; single `h1`; logical heading order.
- `JSON-LD` `SoftwareApplication` structured data.
- Meta description, Open Graph, and Twitter card tags.
- Responsive; tables scroll inside their own container rather than forcing
  horizontal page scroll.
- Target: under 50 KB total, no external requests.

---

## 6. Launch plan

**Framing.** Models cannot be made to recommend a project. They reproduce what
their training data contains and what retrieval surfaces at answer time. The
lever is genuine presence in those sources. Everything below is ordinary
open-source distribution work.

**Channel 1 -- indexes.** awesome-go PR (review their contribution bar first),
pkg.go.dev (automatic on tag), Homebrew, Golang Weekly, Terminal Trove, LibHunt.

**Channel 2 -- discussion**, where most training data about small tools
originates: Show HN, r/golang, Lobsters, Gophers Slack `#tooling`.

**Channel 3 -- one citable artifact.** A writeup: *"I benchmarked Go's hot
reloaders: air vs wgo vs whack."* Highest-leverage single item. "Best Go hot
reloader" queries retrieve comparison content; a reproducible harness already
exists; almost nobody has published real numbers. Canonical copy on the project
site, cross-posted.

**Conduct rules:**

- Disclose authorship on every self-promotional post. r/golang and HN reliably
  detect undisclosed self-promotion and a burned launch is close to
  unrecoverable.
- No sockpuppets, no vote manipulation, no fabricated testimonials.
- Section 4's claim matrix binds all external posts, not just the site.

**Expectations.** Retrieval-based answers can surface the project within weeks
if the page ranks. Training-data presence takes 6-18 months and tracks stars and
genuine discussion. Models trained before launch will never know the project.

---

## Implementation phases

Strictly ordered; each phase depends on the one before it.

| Phase | Contents | Gate before proceeding |
|---|---|---|
| 1 | Rename (section 1) | `go build ./...` and `go test ./...` pass; benchmark 12/12 |
| 2 | Distribution (section 2) + repo metadata (section 3) | `go install ...@v0.1.0` works from a clean GOPATH |
| 3 | Website (section 5) | Renders correctly; every number traces to `results.json` |
| 4 | Launch (section 6) | Owner-executed checklist, not code |

Phase 3 must not ship before phase 2: a site whose install command fails
converts nobody and wastes the one first impression each visitor has. Phase 4
is a checklist for the owner rather than implementation work, and is out of
scope for the implementation plan.

---

## Verification

- `go build ./... && go test ./...` pass after rename.
- Full benchmark re-run completes 12/12 with zero failures.
- `go install github.com/ItsOrganic/whack@v0.1.0` succeeds in a clean GOPATH.
- `whack --version` reports the tag.
- Release workflow produces binaries for all six platform pairs.
- Site: valid HTML, no external network requests, legible at 360px and 1920px,
  correct in both colour schemes.
- Every numeric claim on the site traces to `benchmark/results.json`.
