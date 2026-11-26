package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestRenderConfigValues(t *testing.T) {
	const source = "vars: {zoom: 0.75; d2-config: {scale: ${zoom}; force-appendix: false}}"
	_, config, err := d2compiler.Compile("scale.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.Scale == nil || *config.Scale != 0.75 || config.ForceAppendix == nil || *config.ForceAppendix {
		t.Fatalf("unexpected render config: %+v", config)
	}
	_, config, err = d2compiler.Compile("auto.d2", strings.NewReader("vars: {d2-config: {scale: -1}}"), nil)
	if err != nil || config == nil || config.Scale != nil {
		t.Fatalf("expected automatic sizing, got config %+v, error %v", config, err)
	}

	for _, field := range []string{
		"scale: 0", "scale: -2", "scale: NaN", "scale: +Inf", "scale: 1e1000",
		"scale: 2 {subkey: 1}", "force-appendix: yes", "force-appendix: true {subkey: 1}",
	} {
		t.Run(field, func(t *testing.T) {
			input := "vars: {d2-config: {" + field + "}}"
			key, _, _ := strings.Cut(field, ":")
			_, _, err := d2compiler.Compile("invalid.d2", strings.NewReader(input), nil)
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("expected diagnostic for %s, got %v", key, err)
			}
		})
	}
}

func TestThemeConfig(t *testing.T) {
	for _, tc := range []struct {
		name        string
		light, dark string
		wantLight   int64
		wantDark    int64
	}{
		{"names", "Terminal", "Dark Mauve", 300, 200},
		{"ids", "300", "200", 300, 200},
		{"case and hyphens", "terminal-grayscale", "dark MAUVE", 301, 200},
		{"quoted and substituted names", `"Neutral Default"`, "${night}", 0, 201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "vars: {night: Dark Flagship Terrastruct; d2-config: {theme-id: " + tc.light + "; dark-theme-id: " + tc.dark + "}}"
			_, config, err := d2compiler.Compile("theme.d2", strings.NewReader(src), nil)
			if err != nil {
				t.Fatal(err)
			}
			if config == nil || config.ThemeID == nil || config.DarkThemeID == nil {
				t.Fatalf("missing theme IDs in config: %+v", config)
			}
			if *config.ThemeID != tc.wantLight || *config.DarkThemeID != tc.wantDark {
				t.Errorf("theme IDs = (%d, %d), want (%d, %d)", *config.ThemeID, *config.DarkThemeID, tc.wantLight, tc.wantDark)
			}
		})
	}

	for _, tc := range []struct{ field, value string }{
		{"theme-id", "Missing Theme"},
		{"dark-theme-id", "999"},
	} {
		t.Run("invalid "+tc.field, func(t *testing.T) {
			src := "vars: {d2-config: {" + tc.field + ": " + tc.value + "}}"
			_, _, err := d2compiler.Compile("theme.d2", strings.NewReader(src), nil)
			if err == nil || !strings.Contains(err.Error(), tc.value) {
				t.Fatalf("expected diagnostic for %q, got %v", tc.value, err)
			}
		})
	}
}
