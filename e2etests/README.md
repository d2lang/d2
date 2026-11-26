# e2etests

`e2etests` test the end-to-end flow of turning D2 scripts into a rendered diagram

Tests fall under 1 of 4 categories:

1. **Stable**. Scripts which produce diagrams that never had issues this major release.
2. **Regressions**. Scripts which used to have issues but no longer do. Each one should be
   linked to the PR which fixed it.
3. **Todos**. Scripts which have an issue. If the issue prevents compile, `skip: true` can
   be set, otherwise the issue is visual. Each one should be linked to a Github Issue
   which describes it.
4. **Real-world**. Substantial diagrams from other projects, preserved with source
   revisions, adaptations, and licenses in
   [`testdata/files/REAL_WORLD.md`](testdata/files/REAL_WORLD.md). Each fixture runs
   through Dagre, ELK, and TALA using the same checks as the other e2e groups.

Upon a major release, Regressions are carried over to Stable.

Diagram fixtures have a sketch SVG golden for each layout. The `asciitxtar`
fixtures check SVG, standard ASCII and Unicode text output.

Run a focused case, then inspect its changed images:

```sh
go test ./e2etests -run '^TestE2E/stable/all_shapes$' -count=1
go run ./e2etests/report -skip-tests -delta -test-set '^stable$' -test-case '^all_shapes$'
open ./e2etests/out/e2e_report.html
```

To run one engine, append its subtest name, for example
`go test ./e2etests -run '^TestE2E/stable/all_shapes$/tala$' -count=1`.

After reviewing the changes, accept the selected case's goldens and rerun without
the acceptance flag. Acceptance also updates any changed SVG/board goldens:

```sh
TESTDATA_ACCEPT=1 go test ./e2etests -run '^TestE2E/stable/all_shapes$' -count=1
go test ./e2etests -run '^TestE2E/stable/all_shapes$' -count=1
```

`TA=1` is an alias for `TESTDATA_ACCEPT=1`. `SKIP_SVG_CHECK=1` skips SVG comparisons.

The report displays SVG snapshots. `-delta` includes changed `.got` files and new
images without an expected golden, and ignores byte-identical stale `.got` files.
Omit `-skip-tests` to run tests first; `-timeout 10m` controls that run. `TEST_DIR`
and `REPORT_OUTPUT` override the test directory and report path.
