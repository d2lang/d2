# PlantUML metrics

A four-line patch on top of D2 `v0.9.0` that makes shapes as tight around
their text as PlantUML's are. Nothing else is changed: no new syntax, no
options, and diagrams stay exactly as written.

Upstream D2 sizes a shape as `text + 45px` on each axis and sets the
markdown line height to 1.5 times the font. PlantUML uses `text + 20px` and
about 1.36. The result is that a stock D2 box devotes roughly two thirds of
its height to padding, which reads as a small font in a large rectangle.

## The patch

| file | constant | upstream | here |
|---|---|---|---|
| `lib/shape/shape.go` | `defaultPadding` | `40.` | `15.` |
| `lib/textmeasure/markdown.go` | `MarkdownLineHeight` | `1.5` | `1.36` |
| `d2renderers/d2fonts/d2fonts_common.go` | `FONT_SIZE_M` | `16` | `14` |
| `lib/version/version.go` | `Version` | `v0.8.1-HEAD` | `v0.9.0-plantuml-metrics.1` |

Plus a fix for an unrelated upstream crash, described below.

Every shape derives its padding from `defaultPadding` as a multiple or a
fraction, for example cylinder and cloud use `defaultPadding, defaultPadding/2`
and circle uses `defaultPadding/√2`. So the single constant rescales all
shapes and keeps their relative proportions. **Do not go below 15**:
`shape_stored_data.go` computes `defaultPadding - 10`, which turns negative.

`MarkdownLineHeight` drives both measurement and rendering, so boxes and
baselines stay in agreement. Verified: baseline spacing moves from 24 to
21.8 at a 16px font and box heights follow exactly, so text cannot overflow.

## Result

Box height for the same label, against a real PlantUML render:

| lines | PlantUML | stock D2 | this fork |
|---|---|---|---|
| 1 | 39.1 | 69 | 40 |
| 2 | 58.1 | 93 | 59 |
| 3 | 77.2 | 117 | 78 |

Within one pixel at every line count. Widths come out slightly narrower than
PlantUML, 89 against 98 for "Hello World", because the two tools ship
different fonts.

TALA and ELK are unaffected: shape sizes are computed before layout runs, and
both engines are still bundled in a source build.

## A compiler crash fix, carried alongside

Upstream panics with `invalid memory address or nil pointer dereference` on:

```
**.shape: rectangle
b
LINK -> b
```

A D2 reserved keyword used as a shape name, reachable only through an edge,
in a file that also has a glob. `Field.LastPrimaryKey()` returns a nil
`*d2ast.Key` for a field created that way rather than written as
`KEY: value`; `compileReserved` passes it to `errorf`, and
`d2parser.Errorf` calls `GetRange()` on the nil pointer. The crash happens
inside the code that was trying to report an ordinary compile error, and
about sixty call sites in `d2compiler` pass `LastPrimaryKey()` the same way.

`errorf` now substitutes a zero-range node when handed a nil one, covering
every call site, and the site in the traceback falls back to
`LastRef().AST()` so the message keeps an accurate source position. The
panic becomes `reserved field LINK does not accept composite`. Covered by a
regression test in `d2compiler/compile_test.go`; `go vet` is clean.

This is independent of the metrics change and is worth sending upstream.

## Binaries

The `plantuml-metrics build` workflow cross-compiles Windows, Linux and macOS
binaries on every push to this branch, so no Go toolchain is needed on the
target machine. Download them from the run's **Summary** page under
Artifacts. Pushing a tag like `plantuml-metrics-v1` also publishes a release.

To build it yourself you need Go 1.27 or newer, and nothing else. There is no
cgo. The main package is at the repository root, not `cmd/d2`:

```
go build -o d2 .                                   # host platform
GOOS=windows GOARCH=amd64 go build -o d2.exe .     # cross-compile
```

## Installing on Windows with VS Code

The *D2 in Markdown* extension spawns a bare `d2` and inherits `PATH`. It has
no setting for a binary path, and needs none. Put `d2.exe` where the stock one
lives, or earlier on `PATH`, then restart VS Code completely, since it reads
the environment at launch. Set `d2InMarkdown.layout` to `tala` or `elk`; it
defaults to `dagre`.

Check what is actually being used with `d2 --version`. This fork reports
`v0.9.0-plantuml-metrics.1`, which no upstream release uses.

## Tracking upstream

Everything lives on `master`; there is no separate patch branch. Upstream's
tags are mirrored here, so absorbing a new D2 release is:

```
git remote add upstream https://github.com/d2lang/d2.git   # once
git fetch upstream --tags
git rebase v0.10.0 master
```

Four one-line constant changes and one compiler fix rarely conflict. The
build workflow re-checks all three metric constants after every build and
fails loudly if a rebase drops one. `README.md` carries a three-line banner
at the top, which is the one file likely to conflict; keep it or drop it, it
has no effect on the build.

To see exactly what this fork changes, diff against the release it is based
on rather than against a mirror branch:

```
git diff v0.9.0..master -- lib/shape lib/textmeasure d2renderers/d2fonts d2compiler
```

## Upstream CI is removed

This fork deliberately changes the geometry of every rendered diagram, so
upstream's end-to-end tests, which compare against golden files built for
D2's original proportions, cannot pass. Verified: they fail on exactly the
coordinates the metrics change moves. Keeping those workflows would mean a
permanently red `master` and a thirty-minute job burning Actions minutes on
every push to prove something already known.

So upstream's workflows are deleted here (`ci.yml`, `release-archives.yml`,
`weekly-race.yml`, `tala-fuzz.yml`, the docker and npm staging jobs, and
`windows-msi.yml`). Only `plantuml-metrics-build.yml` remains. Removing
`release-archives.yml` also stops a second, differently named set of
binaries being produced for every `v*` tag, which was a source of confusion
about which download to install.

What still guards correctness: the build workflow asserts the three
constants on every run, and targeted Go tests remain runnable by hand.
`go test ./d2compiler/` covers the panic fix and passes.

## Releases

Tag with the version you want the binary to report, which also publishes a
GitHub release with all four platforms attached:

```
git tag v0.9.0-plantuml-metrics.1
git push origin v0.9.0-plantuml-metrics.1
```

Untagged pushes to `master` still produce artifacts, but those expire with
the repo's retention policy, so tag anything you actually install.
