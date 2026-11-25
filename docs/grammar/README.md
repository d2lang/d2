# D2 syntax reference

This reference defines how to read a D2 source file into a tree of keys,
connections, values and comments. It is versioned with D2. The initial baseline
is the [Go parser at `4e2eeed`](https://github.com/d2lang/d2/blob/4e2eeed49674e39b671a990550c4a25c808d6b25/d2parser/parse.go).
For a particular release, use the reference in that release's source tree when available.

Implementations should follow the productions together with the accompanying
scanner rules. The compatibility notes record places where the baseline parser
accepts additional spellings, loses information, or disagrees with the formatter.

## Reading the grammar

Start at `File` in the [structural grammar](structure.ebnf). It uses the
[lexical grammar](lexical.ebnf) for strings, comments and individual characters.
Read both with the [structural rules](structure.md) and [lexical rules](lexical.md).
These explain how to choose between overlapping forms, decode strings and match
a block's closing delimiter. Whitespace is explicit.

The notation is [Go-style EBNF](https://go.dev/ref/spec#Notation).
A quoted string matches those exact characters, names refer to other rules,
and adjacent terms occur in order. `|` separates alternatives, `[ ]` means
optional, `{ }` means zero or more repetitions, and `( )` groups terms.
A final period ends a rule. A period *in quotes* matches a dot in the input.

These two D2 entries differ only in how they spell the key.

```d2
api.v1: api.v1
"api.v1": api.v1
```

The first key has two path segments, `api` and `v1`. The second key has one
quoted segment, `api.v1`. Both labels contain the literal text `api.v1`.
A scanner needs to know whether it is reading a key or a value before
treating the dot as punctuation.

The tree also preserves distinctions needed by the compiler. For example,
`api.shape: waffle` has a two-part key and an unquoted string value. Parsing
succeeds. The [compiler](../../d2compiler/compile.go) checks whether `waffle`
names a shape. The same division applies to resolving imports, looking up
variables and applying styles.

## Checking an implementation

The [conformance corpus](testdata/conformance.json) is a UTF-8 JSON array.
Parse each record's `source` as a whole D2 file.

- `nodes` gives the expected top-level nodes in source order. Every listed
  object field must match. Arrays match in full, in order. Omitted fields
  (such as source positions) are unconstrained. The field names follow the
  [D2 syntax tree](../../d2ast/d2ast.go). An implementation can use its own
  representation internally.
- `reject: true` requires a syntax error. A parser can still return a partial
  tree for editor use. Diagnostic wording is implementation-specific. A
  `lexicalConstraint` names a production whose scanner rule is needed to
  reject the input, such as the matching closing fence for a block string.

Run the grammar checks from the repository root.

```sh
go test ./d2parser -run '^TestGrammar'
```

The tests verify that every production is defined and reachable from `File`.
They also run the EBNF against the corpus and check the Go parser's trees and
errors. The scanner rules account for rejections marked with `lexicalConstraint`.

Acceptance alone leaves some trees ambiguous. For example, the raw productions
can match `build-server` as either a key path or an edge chain. The key scanner
consumes the internal hyphen, giving one key and zero connections. A second
parser should compare its trees against the expected nodes as well as checking
which inputs it accepts.

When changing D2 syntax, update the affected rules and add an example that
shows how the change affects acceptance or the resulting tree.
