# Structural rules

The [EBNF](structure.ebnf) starts at `File` and consumes the entire input.
A file contains the entries of a map. A nested map adds braces around those
entries. Map entries and array elements use the same separators. A newline or
semicolon ends an entry. The last entry can also end at its container's closing
delimiter, or at end of input in the top-level map. Empty containers, repeated
separators and a trailing separator are valid.

## Entries and values

| D2 | Syntax tree |
| --- | --- |
| `api` | Key with one segment and no assigned value |
| `api.v1: Api v1` | Key with two segments and a scalar value |
| `api { v1 }` or `api: { v1 }` | Key with a map value containing a child key |
| `api: Service { v1 }` | Key with scalar primary value `Service` and a map value |
| `x: [one; two]` | Key with an array of two scalar values |
| `api -> db: writes` | Connection with source `api`, destination `db` and a scalar value |
| `svc.(api -> db)[0].style.stroke: red` | Key path `svc`, a grouped connection, index `0`, property path `style.stroke` and scalar value `red` |

Horizontal space may surround the dot between key segments. A quoted or
escaped dot belongs to its segment. Between segments, a newline ends the path.
A block string used as a segment can itself contain newlines.
An initial `&` or `!&` marks a filter on the whole entry. Unescaped stars in
unquoted key segments preserve glob patterns, including `*`, `**` and `***`.
The compiler applies filters and expands globs in their enclosing scope.

An assignment without a colon takes a map value. A colon requires a value on the same
logical line. When a scalar is followed by a map on that line, preserve both
parts. The same rule applies to a connection label and its properties.

Arrays accept values, including nested maps and arrays. A comma is an ordinary
character in an unquoted string, so `[red, blue]` has one element. A line
comment can follow an entry on the same line. A block comment starts where a
new entry could occur, and another entry may follow its closing triple quote
on the same line. See [lexical rules](lexical.md) for comment and string scanning.

## Connections

The four conventional arrows are `--`, `->`, `<-` and `<->`. An undirected
connection requires at least two hyphens. A chain such as
`a -> b -> c` produces two connections, with `b` as the shared endpoint.
Parentheses allow an index, a property path, or both to follow a chain.
A preceding path scopes the group, as in `svc.(a -> b)[*]`. Decimal indices
start at zero. A star in brackets selects all matching indices.

Read the source key before reading the arrow. A hyphen in an unquoted key
only starts an arrow when the immediately following character is `-`, `>` or
`*`. Thus `a - b` is one key, `a -- b` is a connection, and `a-b -> c` has
the source key `a-b`. A less-than sign ends an unquoted key and starts a
left arrow. A star generally stays in the unquoted key. Refer to the
[lexical rules](lexical.md) for the closing parenthesis in a group.

The arrow productions also describe spellings accepted by the reference
parser. Extra shaft hyphens produce the same endpoints and arrowheads
(`a ---> b` means `a -> b`). A backslash can continue a shaft across one
newline after arrow scanning has begun. Consume the whole shaft before
reading an optional final `>` or `*`. The endpoint marks are recorded
independently. For example, `a < b` has a source arrowhead and `a -* b`
records a destination mark of `*`. An initial star can be read as an arrow
mark inside a group or on a later link in a chain if the preceding key has
already ended, as in `("a" *- "b")[0]`.

## Scalars, substitutions and imports

Quote style is part of the tree. Classify an unquoted scalar only when the
value consists of one literal. Preserve each piece of an interpolated value,
including the literal `10` and the reference to `unit` in `10${unit}`.
After escape decoding, the spellings `null`, `true`, `false`, `suspend` and
`unsuspend` are case-insensitive. The first produces a null value, the next
two produce booleans and the last two produce suspension values. The Go
parser then recognizes a number if the entire spelling denotes a rational
number under the [numeric conversion rules](https://pkg.go.dev/math/big#Rat.SetString).
These include signed numbers, decimal or base-prefixed mantissas, exponents
and integer fractions with an unsigned, nonzero denominator. For example,
`1/2`, `0.5` and `5e-1` have the same numeric value. `1/0` stays an
unquoted string. Quoted `"true"` and `'1/2'` stay strings.

Unquoted and double-quoted values may contain a substitution whose path uses
ordinary key segments, such as `${theme.colors.primary}`. At the start of a
value, `@` introduces an import. Consume the longest run of dots and slashes
immediately after `@` as the path prefix. In `@../theme.d2.colors`, the leading
`../` is a filesystem prefix, `theme` identifies the file, and `colors`
is a selection within it. The parser removes the optional unquoted `.d2`
suffix when the first two segments are unquoted. Quote a filename containing
a literal dot, as in `@"my.theme".colors`.

At the start of a map or array entry, `...` immediately followed by `@` or
`$` introduces a spread. For example, `...@theme` is a whole entry, and
`colors: @theme.colors` assigns an ordinary import. Parsing records the path
and spread flag. File access, variable lookup and the types allowed in a
particular context are determined during compilation.

## Reference parser compatibility

The Go parser accepts some inputs outside the structural productions and
has a few special cases when building the tree for a syntactically valid
input.

| Input | Go parser behavior |
| --- | --- |
| `a.` | Produces the same key path as `a`. |
| `"build" - server` | The baseline parser accepts a single hyphen after the quoted source as an undirected connection. Write `"build" -- server` for the specified form. |
| `(a -> b` at end of file | Returns a connection despite the missing closing parenthesis. |
| `(a -> b); c` | Treats `)` before a semicolon as part of the bare destination and reports an unclosed group. Put `c` on the next line, or write `(a -> "b"); c`. |
| `(a -> b)[0].call(): ok` | Applies the same parenthesis lookahead while reading the property path after the group. Quote the segment as `(a -> b)[0]."call()": ok`. |
| `"svc"(a -> b)[0]` | Accepts the quoted scope without a separating dot. |
| `x: @` or `x: ${}` | Produces an import or substitution with an empty path. |
| `(a -> b)[1 2]` | Reads index 12. A non-ASCII decimal digit may be accepted and decoded as zero. |
| `x: [suspend; unsuspend]` | Recognizes both scalar spellings and omits them from the array tree. |
| `x: true${flag}` | Classifies the first text segment as a boolean, dropping the substitution. An initial numeric or null text segment can similarly determine the scalar type. |

The reference implementation permits up to 128 nested maps and arrays,
excluding the implicit file map, and up to 518 UTF-8 bytes per decoded key
segment.
