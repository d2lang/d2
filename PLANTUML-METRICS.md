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

`master` is untouched and tracks upstream, so a new release is:

```
git remote add upstream https://github.com/d2lang/d2.git   # once
git fetch upstream --tags
git rebase v0.10.0 plantuml-metrics
```

Upstream's tags are mirrored into this fork, so `v0.10.0` resolves without
the extra remote once it has been fetched here.

Four one-line constant changes rarely conflict. The workflow re-checks all
three values after every build and fails loudly if a rebase drops one. The
one file that can conflict is `README.md`, which carries a four-line banner
at the very top; keep it or drop it, it has no effect on the build.

## Releases

Tag with the version you want the binary to report, which also publishes a
GitHub release with all four platforms attached:

```
git tag v0.9.0-plantuml-metrics.1
git push origin v0.9.0-plantuml-metrics.1
```

Untagged pushes still produce artifacts, but those expire with the repo's
retention policy, so tag anything you actually install.
