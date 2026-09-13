package d2latex

import (
	"fmt"
	"unicode/utf8"
)

const (
	// MaxInputBytes bounds the work and output that a single LaTeX label can
	// cause in the embedded MathJax renderer.
	MaxInputBytes = 4 << 10

	// MaxGroupNestingDepth prevents deeply nested TeX groups from exhausting
	// the Go stack in mathjax-go's recursive group parser.
	MaxGroupNestingDepth = 128
)

// ValidateInput applies D2's resource limits to a LaTeX label. Render and
// Measure both call it so compiler, WASM, and raw render-target paths share
// the same admission policy.
func ValidateInput(s string) error {
	if len(s) > MaxInputBytes {
		return fmt.Errorf("latex input is %d bytes, exceeding limit %d", len(s), MaxInputBytes)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("latex input must be valid UTF-8")
	}

	depth := 0
	backslashes := 0
	inComment := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inComment {
			if c == '\n' || c == '\r' {
				inComment = false
			}
			continue
		}
		if c == '\\' {
			backslashes++
			continue
		}

		escaped := backslashes%2 != 0
		backslashes = 0
		if escaped {
			continue
		}

		switch c {
		case '%':
			inComment = true
		case '{':
			depth++
			if depth > MaxGroupNestingDepth {
				return fmt.Errorf("latex group nesting depth %d exceeds limit %d", depth, MaxGroupNestingDepth)
			}
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return nil
}
