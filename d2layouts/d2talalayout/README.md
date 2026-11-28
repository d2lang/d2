# TALA

TALA is a layout and edge-routing engine for D2. `internal/layoutgraph` owns
the mutable graph used during layout, while concrete packages such as
`hierarchy`, `placement`, `packing`, `routing`, and `labeling` own their algorithms.
`internal/placementcost` owns the candidate-geometry scores used by placement
without owning its mutations or search strategy.
`internal/graphbounds` provides the work-accounted node bounds shared by
packing and routing without making either algorithm depend on the other.
`internal/engine` runs those stages in order through a small layout-only API.
It keeps its pipeline stages and snapshots private. `d2talalayout` is the D2
integration boundary, while completed-layout scoring remains internal.
Independent seed attempts use direct in-memory `layoutgraph` clones.

The adapter exposes local, in-process entry points:

- `DefaultLayout` and `Layout` place a D2 graph.
- `Layout` runs every configured seed locally with bounded concurrency, waits
  for every attempt, and selects the best result deterministically.
- `RouteEdges` routes selected D2 edges.
- `ContinueBoard` uses the rendered geometry of the preceding step, or the
  parent of a scenario, to keep familiar objects in place. Each layer starts a
  new layout. A single leaf addition or removal can preserve every existing
  box and wire. For edits to rectangular containers, TALA tries retaining
  surviving members, fitting new members around them, and rerouting affected
  connections. An intact team can carry its internal arrangement to a new
  parent. Candidates are checked for containment, crossings, obstructions,
  and label collisions. More complex geometry can keep a shared origin by
  translating the ordinary layout as a whole. Authored pins, grids, sequences,
  and changes to the root direction or seed settings keep their ordinary
  layout. The CLI and JavaScript compiler enable board continuity for TALA.
  Go callers can register `ContinueBoard` under `"tala"` in
  `d2lib.CompileOptions.LayoutContinuations`. Register `TweenResize` under
  the same key in `LayoutTweens` to animate automatic placement during numeric
  resizes. It uses the completed start and end layouts to place the intermediate
  frames. Every authored board keeps its ordinary boxes, routes, and captions.
- In animated exports, a positive `animate-duration` can draw new connections
  along their routes. A connected node starts opening from its port as the
  incoming stroke covers its last few pixels. Its label appears at its final
  size and position once the body has room. Text blocks, code, classes and SQL
  tables appear at their authored size. A new node can briefly grow past its
  final size when there is room around it. A rectangular container
  can begin tracing its outline as its contents settle, slowing through the
  corners. When one identifiable service or intact team changes parents, its
  external connections can fade while it accelerates and settles into its new
  position. These effects use cumulative steps that share layout coordinates.
  Complex edits can keep the ordinary transition. Set `animate-duration: 0`
  for an immediate change between boards.
- `Options.Seeds` controls deterministic layout attempts. Diagram data
  under `tala-seeds` takes precedence, seeds are de-duplicated in first-seen
  order, and one layout is capped at 16 attempts. Inputs are capped at 64 raw
  seed entries so duplicate-heavy data cannot consume unbounded work.
  `Options.MaxConcurrency` separately bounds the number of live graph clones.
- Layout and routing honor the caller's context; timeout policy belongs to the
  embedding application.

## Tests

From the D2 repository root, run the focused tests with:

```sh
go test ./d2layouts/d2talalayout/...
go test -race ./d2layouts/d2talalayout/...
```

The deterministic seeds registered by the engine's fuzz target and the corpus
under `internal/engine/testdata/fuzz/FuzzAutolayout` run with the normal tests.
To explore new random graphs with Go's built-in fuzzing engine:

```sh
go test ./d2layouts/d2talalayout/internal/engine -run '^$' -fuzz '^FuzzAutolayout$' -fuzztime=30s
```

Run a named synthetic benchmark with:

```sh
BENCHMARK_GRAPH=hierarchy go test ./d2layouts/d2talalayout/internal/engine -run '^$' -bench '^BenchmarkAutolayout$' -benchtime=1x
```

The package tests and synthetic fixtures are part of TALA. Shared layout,
routing, regression, and benchmark fixtures live in `internal/testdata`; the
engine integration tests own layout and OVG golden acceptance. Every layout
case keeps an input and final-output golden, while focused algorithm tests
assert stage invariants without freezing intermediate pipeline output. D2's
end-to-end suite provides an additional integration contract; it is not a
replacement for TALA's focused tests.

## Repository placement

TALA lives in `d2layouts/d2talalayout` alongside D2's other layout engines.
This tree contains the public layout adapter, internal graph and layout
algorithms, focused tests, synthetic fixtures, authorship
information, and applicable third-party notices.

TALA does not carry a separate primary license in D2. Its runtime source is
covered by D2's repository license. Files derived from third-party code retain
their applicable notices and license terms.
