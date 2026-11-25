# D2 lexical rules

A D2 string can be a key or a value. The parser knows which one it expects when it starts reading the string. In the following line the first period separates two parts of a key. The second period and the arrow are ordinary label text.

```d2
api.v1: serves v1.0 -> stable
```

These rules specify how to read the characters matched by the [lexical productions](lexical.ebnf). The [structural productions](structure.ebnf) specify where each token can occur.

## Source text and whitespace

D2 source is Unicode. The file parser reads UTF-8 and also recognizes UTF-16 little-endian input beginning with the bytes `FF FE`. It does not strip a UTF-8 byte order mark. An initial U+FEFF in UTF-8 therefore participates in the first token. The grammar operates on decoded Unicode scalar values. The Go reader replaces ill-formed UTF-8 with U+FFFD when decoding.

`newline` is LF (U+000A). Other Unicode whitespace, including CR, tab, nonbreaking space, and U+2028, belongs to `horizontalSpace`. Thus CRLF has one structural newline, and CR alone does not separate statements. Skip whitespace where the structural grammar permits it. Preserve whitespace within string tokens according to the rules below.

## Comments

A line comment starts at `#` and consumes everything up to LF or end of input. It can follow a map entry or array item on the same line. A hash inside a quoted string, a block string, or an escape is literal. In the syntax tree, the Go parser groups consecutive comment lines separated by a single LF and optional indentation. It drops at most one ASCII space immediately after each opening hash.

A block comment starts with three consecutive double quotes at the start of a map or array item. The first subsequent triple quote closes it, including on the same line. One or two quotes within the body remain literal. Its contents are literal. Another item may follow its closing delimiter on that line. At a scalar value position after a colon, the string rules apply instead. The parser strips opening whitespace through at most the first LF and applies the final-line and indentation rules for blocks below.

## Unquoted strings

Read the longest string allowed in the current position. Skip leading whitespace before the first character and trim trailing Unicode whitespace from the decoded final text segment. Interior spaces remain part of the token. A bare string must contribute text or a substitution. Use `""` or `''` for an empty string.

| Position | What ends the string or changes how it is read |
| --- | --- |
| Key or value | Unescaped LF, `;`, `#`, `{`, `}`, `[`, or `]` ends the string. A backslash introduces an escape or a line continuation. |
| Key | `:`, `.`, `<`, `>`, and `&` delimit the key. `--`, `->`, and `-*` leave the hyphen for the edge parser. Any other hyphen belongs to the name, including at end of input. `build-.job` has the two key segments `build-` and `job`. |
| Value | Periods, colons, ampersands, and arrows are ordinary text. An unescaped dollar starts a substitution. Use `\$` for a literal dollar. |

In both keys and values, the parser records each unescaped `*` as a wildcard alongside the literal pieces of the unquoted string. The compiler uses these patterns when matching keys and filter values. For example, `&label: pre*` matches labels beginning with `pre`. With `&label: "pre*"` or `&label: pre\*`, the star is literal. An ordinary label such as `x: pre*` displays its star.

When the first nonspace character is a single quote, double quote, or pipe, use the corresponding quoted or block string. These characters can occur literally later in a bare string. At the start of a key, an opening parenthesis introduces an edge group where the structure permits one. Parentheses embedded in names such as `fn()` stay in the name. While reading endpoints inside an edge group, a closing parenthesis terminates a bare endpoint if the next nonspace character is `;`, `#`, `{`, `}`, `[`, `]`, `:`, or `.`, or if LF or end of input intervenes. Otherwise it belongs to the name. After the group closes, the property path uses ordinary key rules, so `call()` is one segment in `(a -> b)[0].call(): ok`.

The structural forms for imports, spread operations, maps, and arrays take precedence at their designated positions. A raw `...@` prefix is reserved for spread imports. An unquoted key segment whose *decoded* text begins with `@` is rejected, including `\@name`. Quote the segment to name an object `"@name"`. At the start of a value, `@` introduces an import. A leading `&` or `!&` on a map key introduces a filter.

### Escapes and continuations

In an unquoted or double-quoted string, backslash followed by `a`, `b`, `f`, `n`, `r`, `t`, or `v` gives the corresponding bell, backspace, form feed, line feed, carriage return, tab, or vertical tab. Backslash followed by any other non-LF character yields that character. For example, `\.` produces a literal period, `\\` produces one backslash, and `\u0041` produces the five characters `u0041`. A final, unpaired backslash is an error.

A backslash immediately followed by LF continues the physical line. In bare strings this removes both characters and the following line's indentation. If that line is blank or the input ends, the token ends there. In single- and double-quoted strings it removes only the backslash and LF, preserving the next line's indentation. A backslash before CRLF is followed by CR and therefore does not form this string continuation. Edge operators have their own continuation rule in the structural grammar.

## Quoted strings and substitutions

Double quotes preserve leading and trailing whitespace and use the escapes above. Double-quoted *values* recognize substitutions. Double-quoted keys treat dollar signs and braces literally. In unquoted values and double-quoted values, an unescaped dollar introduces the structural `substitution` production.

Single quotes preserve text literally, including backslashes and dollar signs. Write two adjacent single quotes inside the string for one literal single quote. Backslash immediately followed by LF is the one line-continuation exception. In a quoted string an unescaped LF before the closing quote is an error.

```d2
vars: {name: Ada}
a: "Hello ${name}"
b: 'Hello ${name}'
c: 'Ada''s notebook'
"${name}": A key containing the literal dollar and braces
```

## Block strings

The opening pipe may be followed by a *fence*, a maximal sequence of characters other than whitespace, Unicode letters, Unicode decimal digits, and underscore. The fence may be empty. An optional tag follows immediately. Its first character is a Unicode letter, decimal digit, or underscore. The remaining characters extend to the first Unicode whitespace. Whitespace is required after the opening fence and optional tag. An omitted tag defaults to `md`.

The closing delimiter is the opening fence followed by a pipe. The first occurrence of that delimiter ends the block, even in the middle of a line. A partial match remains body text until the whole delimiter matches. For `|!#md` the terminator is `!#|`, and `##|` is ordinary body text. This matching condition is part of `blockString` in the EBNF. The body is literal, so D2 escapes, comments, and substitutions have no special meaning inside it. For example, this tag is `ts`, the fence is two pipes, and the closing delimiter is three pipes.

```d2
example: |||ts
  const chosen = left || right
|||
```

The parser skips opening whitespace up to and including the first LF, or up to the first same-line body character. For same-line body text it supplies the current indentation (two spaces per nesting level, counting the block). It trims trailing whitespace on the last line, dropping that line's preceding LF if the line was all whitespace. It removes the common leading indentation of the nonblank body lines. Each whitespace character in that indentation counts as one column except a tab, which counts as two. Removing only the first column of a tab leaves one space. If every remaining line is blank, preserve the whitespace left by the final-line trim.

## Go parser compatibility notes

These examples describe the [pinned baseline](README.md). Runtime corrections for them are tracked in [D2 #3043](https://github.com/d2lang/d2/pull/3043).

- For `|!#md`, the baseline parser rejects the specified `!#|` terminator and accepts `##|`. It can also reject repeated non-ASCII fence characters, such as `§§`.
- For `a-.b`, the baseline parser consumes the period into the key segment. It can accept a final hyphen before LF yet report an unterminated connection when the file ends immediately after the hyphen.
- The baseline parser can drop a bare dollar before LF or end of input. Escape a literal dollar as `\$`.
- After a substitution, the baseline formatter can drop an escape. It formats `x: before${v}\#tag` as `x: before${v}#tag`, where `#tag` becomes a comment on reparse.
- When removing common indentation, the baseline parser can split a multibyte whitespace character such as U+2003 (EM SPACE) and produce invalid UTF-8.
