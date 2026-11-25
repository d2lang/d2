package localfile

import "testing"

func TestResolveImportPathOnWindows(t *testing.T) {
	for _, tt := range []struct{ parent, name, want string }{
		{`C:\gallery\radio.d2`, "sub/tideglass.d2", `C:\gallery\sub\tideglass.d2`},
		{`gallery\radio.d2`, "tideglass.d2", `gallery\tideglass.d2`},
		{`\\studio\diagrams\radio.d2`, "sub/tideglass.d2", `\\studio\diagrams\sub\tideglass.d2`},
		{`\\studio\diagrams\sub\radio.d2`, "../tideglass.d2", `\\studio\diagrams\tideglass.d2`},
		{`C:\gallery\radio.d2`, `D:\other\tideglass.d2`, `D:\other\tideglass.d2`},
		{`C:\gallery\radio.d2`, `\\studio\diagrams\tideglass.d2`, `\\studio\diagrams\tideglass.d2`},
	} {
		if got := Unrestricted().ResolveImportPath(tt.parent, tt.name); got != tt.want {
			t.Errorf("ResolveImportPath(%q, %q) = %q, want %q", tt.parent, tt.name, got, tt.want)
		}
	}
}
