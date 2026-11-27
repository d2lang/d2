AI is ripping. Finally got around to clearing out some long-standing issues, formalize a grammar (though it's still wip), fix a bunch of corner cases, tedious issues, etc. I'm focusing on 2 things:
1. Performance -- across size of build (mainly for client-side/JS use case), size of output, and speed. All 3 have advanced significantly in this release, somewhat near capabilities of public frontier models. We'll revisit when the Reimann Hypothesis-solving generation of models come out.
2. Animations. I make a lot of [3b1b-type explainer videos](https://github.com/d2lang/d2/pull/2986) to understand things in my day-to-day work and the quality is tempermental. There's a 3b1b library for their animations but it didn't work well for me and I've removed it from my prompts. I'm more motivated to do well here for my own use case, so I want to make animations in D2 really nice throughout the 0.10 series of releases.

I plan to cut a 1.0 soon finally. There'll be more one-time breaking changes like consolidating casing/hyphenation (e.g. `sequence_diagram` to `sequence-diagram`).

AI-generated changelog:

#### Upgrading from v0.9.0

- **Quote shape names that match keywords.** To keep a child named `STYLE`, change `service.STYLE: notes` to `service."STYLE": notes` and use `api -> service."STYLE"` in connections. Unquoted `LABEL` now sets a label, so an existing diagram can render differently even if it still compiles.
- **Check large generated diagrams.** Dagre and ELK accept up to 1,024 objects and 1,024 edges per layout graph. A flat inventory of 1,025 independent services can use `grid-columns: 32`. For connected diagrams, split the drawing into [boards](https://d2lang.com/tour/composition/).

#### Features 🚀

- **Animated transitions.** Set `animate-duration` to animate styles, gradients, text decorations, grid edits, and sketch shading in SVG or GIF. It defaults to zero. `animate-interval` sets the minimum time per board. [Timing](https://d2lang.com/tour/animation/#configuration) · [Grid examples](https://d2lang.com/tour/animation/#grid-animations)

<a href="https://d2lang.com/img/generated/cache-stampede.svg" aria-label="Open the full-size cache-stampede animation">
  <img src="https://d2lang.com/img/generated/cache-stampede.svg" width="1018" alt="A shield cache combines 1,200 requests from six edge locations into one origin read." />
</a>

[Cache stampede source](https://d2lang.com/tour/animation/)

- **TALA step animations.** Small edits can preserve node positions and routes across steps and scenarios. Connections draw in, groups move between containers, trailing SQL rows slide in or out, and sequence messages travel to their receivers. [Animated steps and source](https://d2lang.com/tour/animation/#animated-steps) · [Go API](https://github.com/d2lang/d2/blob/d70ec718/d2layouts/d2talalayout/README.md#L21-L33)

<a href="https://d2lang.com/img/animation/night-signal.svg" aria-label="Open the full-size Night Signal animation">
  <picture>
    <source media="(prefers-reduced-motion: reduce)" srcset="https://d2lang.com/img/animation/night-signal-still.svg" />
    <img src="https://d2lang.com/img/animation/night-signal.svg" width="1188" loading="lazy" alt="A telescope detects a signal. An outbox saves the receipt and reuses it when the publisher retries." />
  </picture>
</a>

> [!WARNING]
> Animated transitions are in alpha. [Send feedback or suggestions](https://github.com/d2lang/d2/issues/new).

- **Language server.** Run `d2 lsp` for live diagnostics, completion, and formatting in your editor. Go to definition, find references, and rename shapes across open diagrams and their imports. In VS Code, install the official extension with `code --install-extension d2lang.d2lang`. [VS Code guide](https://d2lang.com/tour/vscode/) · [Other editors](https://github.com/d2lang/d2/blob/e53d964/docs/LSP.md)
- **MCP server.** Run `d2 mcp` to let an AI client look up D2 syntax, check diagrams, and render PNG, SVG, or text. Starting with v0.10.0, stable releases publish `io.github.d2lang/d2` to the official MCP Registry after the PyPI package ships. [Set up Codex or Claude Code](https://d2lang.com/tour/mcp/) · [Registry listing manifest](https://github.com/d2lang/d2/blob/e53d964/server.json)
- **ELK.** Set `elk-algorithm` in the diagram. New options control edge routing, layer placement, tree ordering, and spacing. [Compare algorithms](https://d2lang.com/tour/elk/#choose-an-algorithm) · [Layered options](https://d2lang.com/tour/elk/#layered-layout-options)
- **Configuration.** Set `scale` and `force-appendix` in `vars.d2-config`, and `dagre-nodesep` and `dagre-edgesep` in `vars.d2-config.data`. [Export options](https://d2lang.com/tour/vars/#output-scale-and-svg-appendix) · [Dagre spacing](https://d2lang.com/tour/dagre/#spacing)
- **Connections.** Crossing lines now leave a transparent gap in the back line where there’s room. [#3011](https://github.com/d2lang/d2/pull/3011)
- **Themes.** Select light and dark themes by name, for example `theme-id: Terminal` or `--dark-theme dark-mauve`. [Themes](https://d2lang.com/tour/themes/)
- **Syntax.** Use unquoted labels like `cost $5` and `#oncall`, colors like `#abc`, and names like `R&D`. Variables can use keyword names, and nested shapes can be named `layers`, `scenarios`, or `steps`. [Punctuation](https://github.com/d2lang/d2/pull/3054) · [Hash-prefixed labels and keyword names](https://github.com/d2lang/d2/pull/3056)
- **Formatter.** `d2 fmt README.md` formats D2 fences in Markdown. Use `d2 fmt --check --diff README.md` to show a diff in CI. [Formatter options](https://github.com/d2lang/d2/pull/3052)
- **Installation.** Run the CLI with `uvx d2lang`, or install it in CI with `d2lang/setup-d2@v1`. Windows x64 now has a `-user.msi` installer for users without administrator rights. [uv and uvx](https://github.com/d2lang/d2/blob/d70ec718/docs/INSTALL.md#uv-and-uvx) · [GitHub Actions](https://github.com/d2lang/d2/blob/d70ec718/docs/INSTALL.md#github-actions) · [Windows](https://d2lang.com/tour/install/#windows)
- **Linux packages.** Install a specific GitHub release with APT on Debian or Ubuntu (`amd64`, `arm64`) or DNF on Fedora or RHEL (`x86_64`, `aarch64`). Download the matching `.deb` or `.rpm` and pass its local path to the package manager. [Debian and Ubuntu](https://github.com/d2lang/d2/blob/e53d964/docs/INSTALL.md#debian-and-ubuntu) · [Fedora and RHEL](https://github.com/d2lang/d2/blob/e53d964/docs/INSTALL.md#fedora-and-rhel)
- **Language reference.** Explore the grammar with railroad diagrams, EBNF, and parser conformance cases. Download the docs as Markdown, by page or in one file. [Grammar reference](https://d2lang.com/tour/grammar/) · [Conformance cases](https://github.com/d2lang/d2/tree/d70ec718/docs/grammar/testdata) · [Page index](https://d2lang.com/llms.txt) · [Full text](https://d2lang.com/llms-full.txt)

#### Improvements 🧹

**CLI export time** on an Apple M4, including startup, layout, and SVG output. Timings use [`d70ec718`](https://github.com/d2lang/d2/commit/d70ec718).

| Workload | v0.9.0 | v0.10.0 candidate | Speedup |
| --- | ---: | ---: | ---: |
| [3,000 rectangles in a grid](https://github.com/d2lang/d2/pull/2893) | 8.29 s | 159 ms | 52.1× |
| [Ten real-world diagrams](https://github.com/d2lang/d2/blob/d70ec718/e2etests/testdata/files/REAL_WORLD.md), sum of medians | 451 ms | 307 ms | 1.47× |

**Layout and rendering.** Native timings compare each optimization with its preceding implementation. Engine rows total layout time across the test suite. [Layout benchmarks](https://github.com/d2lang/d2/pull/2999)

| Workload | Before | After |
| --- | ---: | ---: |
| Dagre | 298 ms | 22.9 ms |
| ELK | 190 ms | 28.1 ms |
| TALA, one seed, including routing | 11.47 s | 3.08 s |
| [Choose grid row breaks](https://github.com/d2lang/d2/pull/3036), 100 cells in 10 rows | 27.94 ms | 11.38 µs |
| [Compile two SQL tables with a global edge style declared first](https://github.com/d2lang/d2/pull/2979) | 936 µs | 73 µs |
| [Re-render a 1,000-message sequence after a label edit](https://github.com/d2lang/d2/pull/2995) | 24.4 ms | 16.3 ms |
| [Write a 30-slide PowerPoint deck](https://github.com/d2lang/d2/pull/3000) | 3.3 ms | 2.1 ms |

**SVG output size.** Each file uses gzip level 9. The two candidate builds produced identical SVGs for all 16 fixtures with version metadata omitted. [SVG changes](https://github.com/d2lang/d2/pull/3030)

| Diagram | v0.9.0 | v0.10.0 candidate | Reduction |
| --- | ---: | ---: | ---: |
| [Links example](https://github.com/d2lang/d2-docs/blob/03e8b5f0/static/d2/links.d2) with forced appendix | 274.6 KB | 117.8 KB | 57.1% |
| Five [multiboard animations](https://github.com/d2lang/d2-docs/tree/03e8b5f0/static/bespoke-d2) | 37.9 KB | 30.4 KB | 19.6% |
| Ten real-world diagrams | 200.8 KB | 198.9 KB | 1.0% |

**CLI and Docker sizes.** The macOS candidate was built at [`e53d9644`](https://github.com/d2lang/d2/commit/e53d9644).

| Artifact | Before | After | Change |
| --- | ---: | ---: | ---: |
| [Docker layers](https://github.com/d2lang/d2/pull/3003), Linux x86-64, before/after deduplication | 92.16 MB | 54.60 MB | −41% |
| Docker layers, Linux arm64, same change | 86.55 MB | 52.16 MB | −40% |
| macOS arm64 CLI, v0.9.0 → candidate | 40.04 MB | 39.28 MB | −1.9% |
| Linux x86-64 CLI, [PowerPoint writer change alone](https://github.com/d2lang/d2/pull/3000) | 43.47 MB | 40.42 MB | −7% |

- **TALA** aligns connections across containers, centers parallel branches, packs hierarchies more tightly, and routes larger diagrams. [#3034](https://github.com/d2lang/d2/pull/3034) · [#2986](https://github.com/d2lang/d2/pull/2986) · [#3025](https://github.com/d2lang/d2/pull/3025)
- **Grids** balance row widths with `grid-rows` alone and column heights with `grid-columns` alone. [Grid layouts](https://d2lang.com/tour/grid-diagrams/)
- **Exports.** Repeated renders reuse bundled font measurements and subsets. Animated SVG connections respect `prefers-reduced-motion`. Multiboard exports preserve user edits, roll back on failure, and give reserved or nonportable board names hashed filenames. Links between boards follow the new paths. [Font caching](https://github.com/d2lang/d2/pull/2992) · [#2976](https://github.com/d2lang/d2/pull/2976) · [#2900](https://github.com/d2lang/d2/pull/2900)
- **File and image access.** Exports validate image contents and sizes. Use `--allow-private-network` for trusted internal images or `--allow-local-files=false` to disable local reads by diagrams. Go callers must set `CompileOptions.FS` to enable imports. Image access is configured separately. [Go import options](https://github.com/d2lang/d2/blob/d70ec718/d2lib/d2.go#L45-L53) · [Image options](https://github.com/d2lang/d2/blob/d70ec718/lib/imageasset/imageasset.go#L368-L376)
- **Watch mode** authenticates browser sessions through the URL printed by `d2 --watch`. [#2910](https://github.com/d2lang/d2/pull/2910)
- **Installation.** Standalone installs verify SHA-256 checksums. Add `--verify-provenance` to verify signed builds locally. Source builds require Go 1.27.4 or newer. Go module installs report the installed D2 version in the CLI, LSP, and MCP server. [Installer verification](https://github.com/d2lang/d2/blob/d70ec718/docs/INSTALL.md#security) · [Build from source](https://github.com/d2lang/d2/blob/d70ec718/docs/INSTALL.md#from-source)

#### Bugfixes ⛑️

- **CLI exports.** Navigating the watch preview keeps the complete export on disk. Watch resolves Windows imports relative to their diagram. Links open boards with dotted names in SVG, PDF, and PowerPoint. The CLI preserves the source when input and output identify the same file. Text export honors source-selected TALA, and `D2_STDOUT_FORMAT` selects the format for stdout. [#3076](https://github.com/d2lang/d2/pull/3076)

- **Dagre.** Containers keep the same dimensions across runs. Nested diagrams with connections between sibling containers can now render when Dagre previously failed during ordering or edge routing. [Deterministic layouts](https://github.com/d2lang/d2/pull/3066) · [Nested diagrams](https://github.com/d2lang/d2/pull/3070)
- **ELK.** Force, stress, and Mr. Tree route labeled self-loops. These layouts and radial attach cross-container edges to the correct shape or SQL column. Loop labels clear rounded bends where space permits. Mr. Tree routes avoid crossing intervening nodes. [#3016](https://github.com/d2lang/d2/pull/3016) · [#3017](https://github.com/d2lang/d2/pull/3017) · [#3018](https://github.com/d2lang/d2/pull/3018) · [#2981](https://github.com/d2lang/d2/pull/2981)
- **TALA.** Fixed overlapping nodes and captions, reversed dependencies, and misplaced endpoint labels. Chains of `step` shapes retain connection order and annotations. Pinned junctions stay in place. SQL edges attach to the correct row, and routes stay attached when containers move. [#3032](https://github.com/d2lang/d2/pull/3032) · [#3022](https://github.com/d2lang/d2/pull/3022) · [#3028](https://github.com/d2lang/d2/pull/3028) · [#3031](https://github.com/d2lang/d2/pull/3031) · [#2889](https://github.com/d2lang/d2/pull/2889)
- **Grids.** Grids with only `grid-rows` or `grid-columns` finish laying out even when one cell is much larger than the others. Cells keep their order through imports and variable spreads. Circles stay round, nested content fits, and routes retain SQL row targets and make room for self-loops. TALA routes around intervening cells and accepts grid containers as `near` targets. [Grid connections](https://d2lang.com/tour/grid-diagrams/#connections) · [#3036](https://github.com/d2lang/d2/pull/3036)
- **Sequence diagrams.** A nested group containing only a note now renders. Notes after messages between spans on the same actor leave room for the preceding group. [Sequence groups](https://d2lang.com/tour/sequence-diagrams/#groups) · [#3035](https://github.com/d2lang/d2/pull/3035) · [#2940](https://github.com/d2lang/d2/pull/2940)
- **SVG.** Stroke gradients continue through arrowheads. Link and tooltip numbering matches the appendix, including in animated frames. Board changes switch the drawing, links, and keyboard targets together. Scaled exports retain their size with footnotes. SVG text, attributes, links, paint values, and IDs are validated or escaped as appropriate. [#3020](https://github.com/d2lang/d2/pull/3020) · [#2962](https://github.com/d2lang/d2/pull/2962) · [#2964](https://github.com/d2lang/d2/pull/2964) · [#2919](https://github.com/d2lang/d2/pull/2919)
- **Native exports.** SVG icons render with gradients, masks, and blur. PDF backgrounds fill the margins on pages without navigation. PNG watch previews embed local images and show placeholders for missing images. [#2982](https://github.com/d2lang/d2/pull/2982) · [#2932](https://github.com/d2lang/d2/pull/2932) · [#2980](https://github.com/d2lang/d2/pull/2980)
- **Labels.** SQL and class members retain types that match their names. Rounded tables and classes clip their contents to the outline. Fixed LaTeX font colors, paired delimiters, and `bigtimes`. ASCII fixes spacing for single-column multibyte characters such as Vietnamese letters and keeps arrowheads visible under long labels. [SQL tables](https://d2lang.com/tour/sql-tables/) · [UML classes](https://d2lang.com/tour/uml-classes/) · [ASCII export](https://d2lang.com/tour/exports/#ascii)
- **Syntax.** Unquoted reserved keys ignore case. Quote `"STYLE"` for an object name. Labels like `10${unit}` interpolate correctly. Undirected edges require `--`, block fences must match, and invalid groups, indices, filters, and paths report errors. `d2 fmt` preserves board order, import filename case, escapes, and empty maps. `d2 fmt -` writes already-formatted stdin to stdout. [Formatter](https://d2lang.com/tour/auto-formatter/) · [Syntax fixes](https://github.com/d2lang/d2/pull/3043)
- **Board globs.** Globs apply once to each matching layer, scenario, or step, including imported boards. In a `scenarios` map, `*.style.fill: red` sets each scenario’s background. Use `**.style.fill: red` to fill its objects. Glob connections pair endpoints within the same board. [Board glob fixes](https://github.com/d2lang/d2/pull/3071)
- **Variables.** Map spreads apply where they appear, so later assignments win. Templates keep their declaration scope and can style existing connections. Forward references resolve fully, glob filters see substituted values, and Markdown uses the nearest variable binding, including in link URLs and titles. [Markdown variables](https://github.com/d2lang/d2/pull/3057) · [Variable fixes](https://github.com/d2lang/d2/pull/3060) · [Spread substitutions](https://d2lang.com/tour/vars/#spread-substitutions) · [#3044](https://github.com/d2lang/d2/pull/3044)
- **Compiler and API.** Parsing and compilation honor cancellation, including before `d2 fmt` writes. A step with a map can follow one without a map. Glob connections whose shared path matches nothing now compile, and invalid connection endpoints report errors. Cyclic variables, oversized expansions, invalid board trees, and oversized grids return errors. Go callers can configure expansion budgets. Each graph passed to Dagre or ELK is limited to 1,024 objects and 1,024 edges. LaTeX labels are limited to 4 KiB and 128 nested groups. [Compiler and renderer limits](https://github.com/d2lang/d2/blob/d70ec718/ci/release/changelogs/next.md#L108-L158)
- **Windows and standalone installs.** Nested imports resolve relative to the importing file, including on UNC shares. Git Bash can acquire the installer lock. Failed upgrades preserve the existing install. [#2993](https://github.com/d2lang/d2/pull/2993) · [#3026](https://github.com/d2lang/d2/pull/3026) · [#3024](https://github.com/d2lang/d2/pull/3024)
- **Formatter and Go editor.** `d2 fmt` preserves names and labels split across lines. The Go editor distinguishes quoted shape names from properties and keeps label capitalization.
- **Dotted boards and arrays.** Dotted steps and scenarios inherit shapes, connections, and rules. Array spreads preserve declaration order and later entries.
- **Selected imports.** Imported values keep their module bindings and can use caller variables as fallbacks. Variables required only by unselected exports can be omitted.
- **Filters.** Edge filters use each endpoint’s effective label and shape. Class filters accept lists and variables. Nesting depth is correct for shapes named `root`.
- **Animation and Markdown variables.** Numeric variables with metadata interpolate across steps. Templates keep their declaration scope. Markdown substitutions preserve escaped placeholders, shortcut links, and variable names containing Markdown punctuation. [#3063](https://github.com/d2lang/d2/pull/3063)

#### Breaking Changes

- **Layout plugins and postprocessing.** D2 no longer loads external `d2plugin-*` executables. Use a built-in engine. The Go packages `d2plugin` and `d2layouts/d2layoutfeatures` were removed. Drop the plugins argument when calling `d2cli.LayoutResolver` or `d2cli.RouterResolver`. To replace `PostProcess`, modify the bytes returned by `d2svg.Render`. [Layout engines](https://d2lang.com/tour/layouts/#layout-engines) · [Go example](https://github.com/d2lang/d2/blob/d70ec718/docs/examples/lib/1-d2lib/d2lib.go#L28-L35)
- **Go struct changes.** If you set ELK graph options directly, use `EdgeEdgeSpacing` (`*int`) for spacing between edges on adjacent layers. `EdgeNode` now takes a `*float64`. `d2scenebuild.AssetOptions` and `d2themes.ThemableElement` contain slice or map fields. Code that compares them with `==` or uses them as map keys must compare or key on the fields it needs.

<a name="d2js"></a>

#### D2.js 0.2.0

The `@d2lang/d2` package runs in browsers and Node.js.

- **Command line.** Run `npx @d2lang/d2 input.d2 output.svg` to render a single file. Omit the output path to write an SVG beside the input, or pipe source to the command for SVG on stdout. To keep a native PNG or PDF export in an npm script, invoke the native binary by its full path. [CLI usage](https://github.com/d2lang/d2/tree/644077c8/d2js/js#command-line)
- **API.** `d2.renderSVG()` renders a D2 string or a multi-file project in one call. `d2.format()` formats a source file. TypeScript now covers theme overrides and the Playground helpers `d2.encode()` and `d2.decode()`. Decoded source is limited to 16 MiB. [JavaScript API](https://github.com/d2lang/d2/tree/e53d9644/d2js/js#api-reference)
- **Animations and fonts.** Set `animateDuration` for step transitions. Use `forceAppendix` with `animateInterval` to show each board’s links and tooltips in its frame. Custom TrueType fonts supplied as `Uint8Array` now render correctly. Compiled diagrams retain their fonts when another diagram uses a different family or a saved diagram is rendered in a fresh worker. [Animation options](https://d2lang.com/tour/animation/#configuration)
- **Browser workers.** Call `new D2()` inside a dedicated worker, or pass `{ workerURL }` to load a self-hosted worker. Safari 27.2 workers start correctly. The SDK surfaces startup errors and releases the embedded worker URL after startup, including on failure. [Worker options](https://github.com/d2lang/d2/blob/e53d9644/d2js/js/README.md#L119-L139)
- **Nightlies.** Install the latest nightly with `npm install @d2lang/d2@nightly`.

**Package size.** Removing the goroutine scheduler and deduplicating fonts and tables cut the WebAssembly module by 53% compared with the preceding build. It still includes all three layout engines. All 24,824 outputs matched in the 519-script comparison. Building from source requires D2’s TinyGo fork. Sizes were measured at `e53d9644`. [Build instructions](https://github.com/d2lang/d2/tree/d70ec718/d2js/js#development) · [Build changes and comparison](https://github.com/d2lang/d2/pull/3046)

| Artifact | 0.1.34 (npm) | Before #3046 | 0.2.0 candidate | Reduction vs. 0.1.34 |
| --- | ---: | ---: | ---: | ---: |
| npm download | 31.66 MB | 13.65 MB | 8.28 MB | 73.8% |
| Unpacked npm package | 95.88 MB | 30.41 MB | 15.85 MB | 83.5% |
| WebAssembly, uncompressed | 42.12 MB | 22.49 MB | 10.59 MB | 74.8% |
| WebAssembly, Brotli-compressed inside browser module | 8.50 MB | 5.58 MB | 3.58 MB | 57.9% |
| Complete browser module, HTTP Brotli quality 5 | 8.52 MB | 5.64 MB | 3.65 MB | 57.1% |

**Chrome performance.** Median times on an Apple M4 at [`d70ec718`](https://github.com/d2lang/d2/commit/d70ec718), with the browser module served from localhost. WebAssembly now decompresses in the worker, freeing the main thread during startup. The first SVG uses a fresh worker. The ten-diagram run reuses one worker for all ten compiles and renders. [Benchmark diagrams](https://github.com/d2lang/d2/blob/d70ec718/e2etests/testdata/files/REAL_WORLD.md)

| Workload | 0.1.34 (npm) | Before #3046 | 0.2.0 candidate | Speedup vs. 0.1.34 |
| --- | ---: | ---: | ---: | ---: |
| First two-node SVG, including module download and import | 765 ms | 296 ms | 191 ms | 4.00× |
| First two-node SVG, after module import | 714 ms | 262 ms | 167 ms | 4.27× |
| [Main-thread delay during startup](https://github.com/d2lang/d2/pull/2984) (zero-delay timer) | 501 ms | 1.9 ms | 1.5 ms | — |
| Ten real-world diagrams, warm worker | 748 ms | 310 ms | 208 ms | 3.59× |

---

[Install D2](https://d2lang.com/tour/install/) · [All changes since v0.9.0](https://github.com/d2lang/d2/compare/v0.9.0...d10e34079)
