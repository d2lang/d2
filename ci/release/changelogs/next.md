#### Features 🚀

- Set `animate-duration` in `vars.d2-config` to animate changes across steps in SVG and GIF exports. Set numeric properties or colors directly, or drive them with variables. Gradients with the same kind, direction, and stop count interpolate each stop’s color and position.

- Animate grid edits between steps with the same `animate-duration` setting. Cells slide to their new positions, arrivals grow in, and departures shrink away. An inserted row or column opens as a group. A replacement can hand off in place, and a summary cell can unfold to show a nested grid.

- TALA keeps familiar nodes and links in place across compatible edits to steps and scenarios. With `animate-duration`, new connections can draw toward arriving nodes, containers can form around their contents, services can move between containers, SQL rows can drop into place, and sequence messages can travel to their receivers.

- Set `scale` and `force-appendix` in `vars.d2-config`. Scaled SVGs and raster exports now retain their requested display size when tooltips or links add an appendix.
- Configure Dagre's node and edge spacing in `vars.d2-config.data` with `dagre-nodesep` and `dagre-edgesep`.

- Set the time each board is displayed in an animated SVG or GIF directly in the diagram with `vars: {d2-config: {animate-interval: 800ms}}`. Override it at export time with `--animate-interval`.

- elk: choose the layout algorithm in `vars.d2-config.data`. Configure the force model and repulsion, target edge length for stress, tree traversal and sibling order, and radial spacing, branch angles and compaction in D2 or on the CLI. Layered layout also supports edge merging and routing, spacing within layers, pipeline wrapping, declaration order and feedback edges. Set `elk-layer-constraint` on a shape to place it in the first or last layer.

- Windows users can install D2 without administrator rights by choosing the `-user.msi` download.
- elk: configure the spacing between edges routed between the same pair of layers with `--elk-edgeEdgeBetweenLayers`. It was previously fixed at 50, which forced large gaps between layers on graphs with many links between adjacent layers.

#### Improvements 🧹

- Parser authors can use the [D2 syntax reference](https://github.com/d2lang/d2/tree/master/docs/grammar) and its shared conformance cases to check their implementations.

- Grids with one specified axis choose more evenly sized rows or columns. For common integral cell sizes the partition search finds the minimum total deviation from the average track length.

- Standalone installs check the archive against a SHA-256 verified during release review. Use `--verify-provenance` to also check the signed build provenance locally. [#315](https://github.com/d2lang/d2/issues/315)

- cli: `--allow-local-files=false` (or `$D2_ALLOW_LOCAL_FILES=false`) denies every local-file access, so imports and local images fail instead of reading files on the host. Use it with the default network policy to render untrusted diagrams. [#3002](https://github.com/d2lang/d2/pull/3002)

- performance: layouts are faster with identical output: Dagre about 13× and ELK about 7× in the end-to-end suite (17.5× and 10.7× for the layout calls alone), and about 12× each in D2.js. TALA lays out about 3.7× faster, and a few of its layouts change slightly. [#2999](https://github.com/d2lang/d2/pull/2999)

- Ordinary labeled diagrams render faster and allocate fewer bytes because the SVG writer records fonts and theme colors as it draws. [#2995](https://github.com/d2lang/d2/pull/2995)

- Build D2 with Go 1.27.4 and update its Go dependencies, including Brotli and the Go networking libraries. Source installs require Go 1.27.4 or newer.

- D2.js: the WebAssembly module is built with TinyGo instead of Go, reducing its Brotli-compressed size by about 40%. [#2966](https://github.com/d2lang/d2/pull/2966)

- D2.js: the TinyGo-built WebAssembly module is a further 11.2 MB (50.2%) smaller, and 1.8 MB (32.7%) smaller when compressed with Brotli, and runs D2's end-to-end corpus in about half the time, with identical output. D2.js calls into the module and every call runs to completion, so the module is now built without a goroutine scheduler, which on WebAssembly rewrote every function that could be running when a goroutine is switched out and made the code about two thirds larger. The build needs TinyGo from [d2lang/tinygo](https://github.com/d2lang/tinygo/releases). The module also drops its function names, a second HTML entity table, two unused Unicode segmentation tables and four separately encoded copies of bundled fonts, stores the tables of the formula renderer and of the ELK layout engine as constant data, keeps the formula renderer's glyph outlines compressed until a formula needs them and its bundled fonts compressed until it starts, and runs D2's own sorts through one shared routine. [#3046](https://github.com/d2lang/d2/pull/3046)

- The d2 binary is about 3 MB smaller: PPTX export writes its XML parts without `text/template`, whose method lookups by name kept unused methods throughout the program. [#3000](https://github.com/d2lang/d2/pull/3000)

- TALA routes larger cross-container diagrams within its existing search budget. It preserves the original hierarchy lanes on compact diagrams and indexes previously routed segments. [#2891](https://github.com/d2lang/d2/issues/2891)

- Processes that render more than once (watch mode, D2.js, the Go API) reuse the glyph metrics and embedded-font subsets of the bundled fonts, held in bounded caches of about 10 MB and 8 MB. Output is unchanged. [#2992](https://github.com/d2lang/d2/pull/2992)

- d2svg:
  - Prevent malformed diagram or raw render values from breaking out of SVG text
    and attributes. [#2919](https://github.com/d2lang/d2/pull/2919)
  - Encode unsafe local IDs consistently so definitions and references continue
    to match. [#2919](https://github.com/d2lang/d2/pull/2919)
  - Disable animated connection CSS animations when
    `prefers-reduced-motion: reduce` is active. [#1979](https://github.com/d2lang/d2/issues/1979)
- assets:
  - Apply the same local/network access policy and cumulative resource budgets to
    SVG and raster exports. [#2918](https://github.com/d2lang/d2/pull/2918)
  - Accept only valid PNG, JPEG, GIF, WebP, and SVG resources during image
    bundling; malformed data and other formats now fail instead of being embedded
    based only on a response `Content-Type`. [#2918](https://github.com/d2lang/d2/pull/2918)
  - Block private and reserved network destinations by default; trusted fetching
    requires `--allow-private-network` or `D2_ALLOW_PRIVATE_NETWORK`. [#2899](https://github.com/d2lang/d2/pull/2899)
  - Require Go API callers to choose an explicit rooted or trusted unrestricted
    policy for local imports and image assets. CLI and watch retain trusted
    local-file access. [#2901](https://github.com/d2lang/d2/pull/2901)
- Remove the legacy `d2plugin` system; Dagre, ELK, and TALA remain built in. [#2920](https://github.com/d2lang/d2/pull/2920)
- watch: authenticate HTTP and WebSocket access with a fresh per-process
  capability, exchanged for a token-free browser session. [#2910](https://github.com/d2lang/d2/pull/2910)
- exports:
  - Map unsafe board names to bounded portable output paths. [#2900](https://github.com/d2lang/d2/pull/2900)
  - Publish multiboard output transactionally within the selected output root. [#2900](https://github.com/d2lang/d2/pull/2900)
  - Preserve unrelated or user-modified files when generated output is updated. [#2900](https://github.com/d2lang/d2/pull/2900)
  - Prevent concurrent local path replacement from redirecting publication
    outside the selected tree. [#2908](https://github.com/d2lang/d2/pull/2908)

#### Bugfixes ⛑️

- `d2 fmt` preserves imported boards, board declaration order, explicit empty maps, block comments in arrays, and the parentheses around connections such as `("api" *- "db")`.
- A file ending in the object name `!` parses with or without a final newline. Standalone key and value parsers reject trailing input, including an editor target such as `a; b`.
- Empty filters (`&` or `!&`) now report an error. Supply a condition such as `&shape: circle`. A trailing dot needs a path segment. Write `a` for the object or `"a."` for a name ending in a literal dot. Use `"svc".(a -> b)[0]` to scope a connection.
- A lone hyphen after a quoted name now reports a syntax error. Change `"build" - server` to `"build" -- server`. An unquoted name such as `build-server` still names one object.
- Reserved keys work in any letter case. For example, `service.STYLE.Stroke: blue` sets the stroke color when you compile the diagram directly. To keep an object named `STYLE`, write `"STYLE"`.
- Unquoted labels such as `10${unit}` resolve their substitutions even when the prefix looks like a number, boolean, or other special value. An undefined variable in that position now reports an error.
- `d2 fmt` preserves escaped label text after a substitution, including the literal hash in `before${v}\#tag`.
- Grouped connections require a closing `)` and at least one connection. A semicolon can follow the group, and names such as `call()` work in its property path. Invalid or oversized edge indices report errors.
- Block strings close at the exact opening fence followed by a pipe, including mixed and Unicode fences. For an opener of `|!#md`, use `!#|` to close. Block strings and comments preserve Unicode text when removing indentation.
- A name ending in `-` keeps its final hyphen before punctuation or end of file. An incomplete substitution or empty import now gives a diagnostic. Write `\$` for a literal dollar in a label.
- Formatting preserves label capitalization, quoted objects named `"layers"`, `"scenarios"`, or `"steps"`, case-sensitive import filenames, and imports selecting a field named `d2`. Arrays retain `suspend` and `unsuspend` elements.

- `d2 fmt` puts top-level entries on separate lines on the first pass, including files with no final newline. Nested maps can still use semicolons.

- Grids retain cell order across repeated dimensions, imports and variable spreads. [#2115](https://github.com/d2lang/d2/issues/2115)
- Grid cells preserve shape proportions and keep nested children, routed connections and their captions inside the container. Border icons and labels fit in zero-padding exports. [#1405](https://github.com/d2lang/d2/issues/1405)
- Grid connections retain SQL row attachments as cells move, separate reciprocal captions, and make room for self-loops between neighboring cells. TALA routes around intervening cells and accepts a grid container as a `near` target. [#1741](https://github.com/d2lang/d2/issues/1741)

- Standalone upgrades and forced reinstalls preserve the working installation if the replacement fails. The installer rejects install-only flags with `--uninstall` and falls back to the GitHub API when the latest-release redirect fails. [#84](https://github.com/d2lang/d2/issues/84)

- TALA keeps hierarchy nodes and containers separated, preserves directed dependencies when orienting cycles and undirected links, and respects nested directions, container dimensions, edge labels, and proximity hints.

- Windows: resolve nested imports relative to the importing diagram, including on UNC shares. [#2642](https://github.com/d2lang/d2/issues/2642)

- PNG, GIF, PDF and PPTX exports now render SVG icons that use default gradients, masks, Gaussian blur and common editor metadata. [#2937](https://github.com/d2lang/d2/issues/2937)

- d2.js can render diagrams in Safari 27.2 workers again. [#2956](https://github.com/d2lang/d2/issues/2956)
- compiler:
  - Apply recursive connection styles efficiently to diagrams with few or no
    connections, including large SQL tables. [#2946](https://github.com/d2lang/d2/issues/2946)
  - Limit combined map/array nesting to 128. [#2894](https://github.com/d2lang/d2/pull/2894)
  - Honor cancellation during parsing and IR compilation, and prevent `d2 fmt`
    from writing output after cancellation. [#2894](https://github.com/d2lang/d2/pull/2894)
  - Limit active import chains to 128. [#2904](https://github.com/d2lang/d2/pull/2904)
  - Reject cyclic composite variables with source diagnostics instead of allowing
    unbounded recursion. [#2907](https://github.com/d2lang/d2/pull/2907)
  - Limit variable substitution output and automatic board, import, and class
    copies to a 65,536-unit compilation budget by default; Go callers can
    configure the limit. [#2923](https://github.com/d2lang/d2/pull/2923)
  - Honor cancellation while expanding substitutions and materializing compiled
    graphs. [#2923](https://github.com/d2lang/d2/pull/2923)
  - Limit recursive glob matching and generated-field work so small diagrams
    fail with a clear error instead of consuming excessive CPU or memory.
    [#2925](https://github.com/d2lang/d2/pull/2925)
  - Limit wildcard edge connections and selectors to bounded expansion work,
    returning a clear error before excessive fanout consumes resources.
    [#2926](https://github.com/d2lang/d2/pull/2926)
  - Keep `sql_table` column types that match the column name, such as
    `date: date`, instead of rendering an empty type. [#2950](https://github.com/d2lang/d2/pull/2950)
  - Keep `class` field types and method return types that match the name, such
    as `date: date`, instead of dropping them. [#2952](https://github.com/d2lang/d2/pull/2952)
- watch: embed local images in PNG watch previews instead of treating their URLs
  as board navigation. Unavailable images show the same placeholder in the
  preview and the exported PNG. [#2945](https://github.com/d2lang/d2/issues/2945)
- decoding and assets:
  - Cap decompressed URL-encoded D2 input at 16 MiB. [#2902](https://github.com/d2lang/d2/pull/2902)
  - Bound image references, locators, fetched and decoded bytes, cached data, and
    final output during SVG image bundling. [#2911](https://github.com/d2lang/d2/pull/2911)
- ELK moves a self-loop label beside the edge when it would cover a bend and there is room.
- elk: fix Mr. Tree arrows doubling back through their target shapes and shortcut edges cutting through intervening nodes. [#2981](https://github.com/d2lang/d2/pull/2981)
- elk: route self-loops in Stress, Force and Mr. Tree layouts, including labeled loops and multiple loops on the same shape. [#2997](https://github.com/d2lang/d2/issues/2997)
- elk: route arrows across container boundaries in Stress, Force, Mr. Tree and Radial, including connections to SQL columns. [#2717](https://github.com/d2lang/d2/issues/2717)
- rendering:
  - Reject malformed, cyclic, reused, or nil raw diagram board graphs with
    ordinary errors instead of panics or fatal recursion. [#2912](https://github.com/d2lang/d2/pull/2912)
  - Validate raw geometry, routes, arrows, fonts, and arithmetic before recursive
    rendering. [#2912](https://github.com/d2lang/d2/pull/2912)
  - Limit render targets to 1,024 levels and 16,384 boards, including animation samples. Longer grid animations share the available samples across transitions. [#2912](https://github.com/d2lang/d2/pull/2912) [#3021](https://github.com/d2lang/d2/pull/3021)
  - Limit LaTeX labels to 4 KiB and 128 nested groups. [#2922](https://github.com/d2lang/d2/pull/2922)
  - Render LaTeX labels with the shape's font color instead of its stroke color. [#2943](https://github.com/d2lang/d2/pull/2943)
  - Limit grids to 10,000 rows or columns and 1,000,000 total cells, returning
    a clear error for oversized dimensions instead of crashing or exhausting
    memory. [#2924](https://github.com/d2lang/d2/pull/2924)
  - Limit Dagre and ELK inputs to 1,024 objects and 1,024 edges, and reject
    overly interconnected graphs before entering non-cancellable layout.
    [#2926](https://github.com/d2lang/d2/pull/2926)
- links and paints:
  - Reject dangerous ordinary link schemes after decoding common obfuscation. [#2896](https://github.com/d2lang/d2/pull/2896)
  - Require gradient stop positions to be finite numbers or percentages. [#2905](https://github.com/d2lang/d2/pull/2905)
  - Reject external SVG paint URLs and unsafe fill-pattern or theme values in raw
    render targets. [#2916](https://github.com/d2lang/d2/pull/2916)
  - Apply the dangerous-link policy to legend shapes. [#2917](https://github.com/d2lang/d2/pull/2917)
- d2svg:
  - Escape custom shape and connection classes. [#2895](https://github.com/d2lang/d2/pull/2895)
  - Sanitize bundled-image media types and escape serialized image URLs. [#2898](https://github.com/d2lang/d2/pull/2898)
  - Escape SQL-table constraint text. [#2903](https://github.com/d2lang/d2/pull/2903)
  - Encode and escape clip-path identifiers consistently in definitions and
    references. [#2906](https://github.com/d2lang/d2/pull/2906)
  - Escape color attributes across shapes, connections, labels, and themes. [#2909](https://github.com/d2lang/d2/pull/2909)
- d2ascii:
  - Measure label widths in runes instead of bytes, so multi-byte characters
    (e.g. Vietnamese) no longer get extra spaces inserted inside words and
    labels are centered and sized correctly. [#2963](https://github.com/d2lang/d2/pull/2963)
  - Keep horizontal arrowheads visible when an edge label fills the space between endpoints.
- appendix:
  - Keep numbered link and tooltip markers in one sequence with the appendix:
    every link marker now has an entry, falling back to the link when no board
    label is available (e.g. `link: _` in a scenario). [#2961](https://github.com/d2lang/d2/issues/2961)
  - Render forced appendices inside their animated SVG frames and size the
    animation to include every board's footnotes.
- sequence diagrams: fix a compile error ("invalid position with infinity value") when a group nested in another group contains only a note. [#2750](https://github.com/d2lang/d2/issues/2750)

---

For the latest d2.js changes, see separate [changelog](https://github.com/d2lang/d2/blob/master/d2js/js/CHANGELOG.md).
