package d2ir

import (
	"strings"

	"github.com/d2lang/d2/d2ast"
)

func (m *Map) multiGlob(pattern []string, visit func(*Field) bool) ([]*Field, bool) {
	var fa []*Field
	if d2ast.IsDoubleGlob(pattern) {
		m._doubleGlob(&fa, visit)
		return fa, true
	}
	if d2ast.IsTripleGlob(pattern) {
		m._tripleGlob(&fa, visit)
		return fa, true
	}
	return nil, false
}

// pathToField returns the source-ordered field path from m to target. A nil
// result means target is outside m's subtree.
func (m *Map) pathToField(target *Field) []*Field {
	var reverse []*Field
	for current := target; current != nil; {
		parent := ParentMap(current)
		if parent == nil {
			return nil
		}
		reverse = append(reverse, current)
		if parent == m {
			path := make([]*Field, len(reverse))
			for i := range reverse {
				path[len(reverse)-1-i] = reverse[i]
			}
			return path
		}
		current = ParentField(parent)
	}
	return nil
}

func (m *Map) directChildToward(target *Field) *Field {
	path := m.pathToField(target)
	if len(path) == 0 {
		return nil
	}
	return path[0]
}

// multiGlobMatchesToward returns the fields in the changed top-level branch
// toward target that the existing recursive glob traversal would append. A
// whole branch is replayed (rather than only the target path) so reference and
// filter ordering remains identical within the affected branch.
func (m *Map) multiGlobMatchesToward(target *Field, pattern []string, visit func(*Field) bool) []*Field {
	path := m.pathToField(target)
	if len(path) == 0 {
		return nil
	}
	var matches []*Field
	if d2ast.IsDoubleGlob(pattern) {
		_doubleGlobField(path[0], &matches, visit)
		return matches
	}
	if d2ast.IsTripleGlob(pattern) {
		_tripleGlobField(path[0], &matches, visit)
		return matches
	}
	return nil
}

func _doubleGlobField(f *Field, matches *[]*Field, visit func(*Field) bool) bool {
	if f == nil || f.Name == nil {
		return true
	}
	if visit != nil && !visit(f) {
		return false
	}
	if name := d2ast.Keyword(f.Name); name != "" {
		if skipRecursiveGlobSubtree(name, false) {
			return true
		}
		if f.Map() != nil {
			return f.Map()._doubleGlob(matches, visit)
		}
		return true
	}
	if NodeBoardKind(f) == "" {
		*matches = append(*matches, f)
	}
	if f.Map() != nil {
		return f.Map()._doubleGlob(matches, visit)
	}
	return true
}

// name is an unquoted reserved keyword.
func skipRecursiveGlobSubtree(name string, triple bool) bool {
	_, board := d2ast.BoardKeywords[name]
	if triple {
		return !board
	}
	return board || name == "classes" || name == "vars"
}

func _tripleGlobField(f *Field, matches *[]*Field, visit func(*Field) bool) bool {
	if f == nil || f.Name == nil {
		return true
	}
	if visit != nil && !visit(f) {
		return false
	}
	if name := d2ast.Keyword(f.Name); name != "" {
		if skipRecursiveGlobSubtree(name, true) {
			return true
		}
		if f.Map() != nil {
			return f.Map()._tripleGlob(matches, visit)
		}
		return true
	}
	if NodeBoardKind(f) == "" {
		*matches = append(*matches, f)
	}
	if f.Map() != nil {
		return f.Map()._tripleGlob(matches, visit)
	}
	return true
}

func (m *Map) _doubleGlob(fa *[]*Field, visit func(*Field) bool) bool {
	for _, f := range m.Fields {
		if visit != nil && !visit(f) {
			return false
		}
		if f.Name == nil {
			continue
		}
		if name := d2ast.Keyword(f.Name); name != "" {
			if skipRecursiveGlobSubtree(name, false) {
				continue
			}
			if f.Map() != nil {
				if !f.Map()._doubleGlob(fa, visit) {
					return false
				}
			}
			continue
		}
		if NodeBoardKind(f) == "" {
			*fa = append(*fa, f)
		}
		if f.Map() != nil {
			if !f.Map()._doubleGlob(fa, visit) {
				return false
			}
		}
	}
	return true
}

func (m *Map) _tripleGlob(fa *[]*Field, visit func(*Field) bool) bool {
	for _, f := range m.Fields {
		if visit != nil && !visit(f) {
			return false
		}
		if f == nil || f.Name == nil {
			continue
		}
		if name := d2ast.Keyword(f.Name); name != "" {
			if skipRecursiveGlobSubtree(name, true) {
				continue
			}
			// We don't ever want to append layers, scenarios or steps directly.
			if f.Map() != nil {
				if !f.Map()._tripleGlob(fa, visit) {
					return false
				}
			}
			continue
		}
		if NodeBoardKind(f) == "" {
			*fa = append(*fa, f)
		}
		if f.Map() != nil {
			if !f.Map()._tripleGlob(fa, visit) {
				return false
			}
		}
	}
	return true
}

// ScalarString remains available for typed filters such as &level. Label
// filters also use the original wildcard boundaries of an interpolated operand.
type interpolatedGlobOperand struct {
	d2ast.Scalar
	*interpolatedGlobPattern
}

type interpolatedGlobPattern struct {
	literals []string
}

func filterScalarValue(s *Scalar, operand bool) d2ast.Scalar {
	if operand && s.globPattern != nil {
		return interpolatedGlobOperand{s.Value, s.globPattern}
	}
	return s.Value
}

func (p *interpolatedGlobPattern) matches(value string) bool {
	value = strings.ToLower(value)
	prefix := strings.ToLower(p.literals[0])
	rest, ok := strings.CutPrefix(value, prefix)
	if !ok {
		return false
	}
	for _, literal := range p.literals[1 : len(p.literals)-1] {
		_, rest, ok = strings.Cut(rest, strings.ToLower(literal))
		if !ok {
			return false
		}
	}
	return strings.HasSuffix(rest, strings.ToLower(p.literals[len(p.literals)-1]))
}

// Keep slices of the coalesced text between authored wildcards. An inserted or
// escaped star is part of a literal, even if that literal is exactly "*".
func makeInterpolatedGlobPattern(text string, boxes []d2ast.InterpolationBox) *interpolatedGlobPattern {
	var literals []string
	start, offset := 0, 0
	for _, box := range boxes {
		if box.String == nil {
			break
		}
		end := offset + len(*box.String)
		if box.Substitution == nil {
			raw := *box.String
			if box.StringRaw != nil {
				raw = *box.StringRaw
			}
			// An escape consumes one extra backslash in raw. Its operand
			// occupies the same number of UTF-8 bytes in the decoded string.
			for i, pos := 0, offset; i < len(raw) && pos < end; i, pos = i+1, pos+1 {
				if raw[i] == '\\' && i+1 < len(raw) {
					i++
				} else if raw[i] == '*' {
					literals = append(literals, text[start:pos])
					start = pos + 1
				}
			}
		}
		offset = end
	}
	if literals == nil {
		return nil
	}
	return &interpolatedGlobPattern{literals: append(literals, text[start:])}
}

func matchPattern(s string, pattern []string) bool {
	if len(pattern) == 0 {
		return true
	}
	s = strings.ToLower(s)
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == "*" {
			if i == len(pattern)-1 {
				return true
			}

			next := strings.ToLower(pattern[i+1])
			if i+1 == len(pattern)-1 {
				return strings.HasSuffix(s, next)
			}

			// Match the earliest occurrence so the remaining wildcards have the
			// largest possible suffix to search.
			j := strings.Index(s, next)
			if j == -1 {
				return false
			}
			s = s[j+len(next):]
			i++
		} else {
			part := strings.ToLower(pattern[i])
			if !strings.HasPrefix(s, part) {
				return false
			}
			s = s[len(part):]
		}
	}
	return s == ""
}
