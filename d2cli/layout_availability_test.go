package d2cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyDiagramRejectsUnavailableLayouts(t *testing.T) {
	for _, tc := range []struct {
		engine  string
		enabled bool
	}{
		{engine: "external"},
		{engine: "dagre", enabled: dagreEnabled},
		{engine: "elk", enabled: elkEnabled},
	} {
		if tc.enabled {
			continue
		}
		engine := tc.engine
		t.Run(engine, func(t *testing.T) {
			for _, sourceSelection := range []bool{false, true} {
				name := "flag"
				if sourceSelection {
					name = "source"
				}
				t.Run(name, func(t *testing.T) {
					directory := t.TempDir()
					input := ""
					args := []string{"--layout=" + engine, "input.d2", "output.svg"}
					if sourceSelection {
						input = fmt.Sprintf("vars: {d2-config: {layout-engine: %s}}\n", engine)
						args = []string{"input.d2", "output.svg"}
					}
					if err := os.WriteFile(filepath.Join(directory, "input.d2"), []byte(input), 0600); err != nil {
						t.Fatal(err)
					}
					_, err := runLayoutCLI(t, directory, nil, args...)
					want := fmt.Sprintf("D2_LAYOUT %q is not a supported built-in layout engine", engine)
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("empty diagram error = %v, want %q", err, want)
					}
					if _, err := os.Stat(filepath.Join(directory, "output.svg")); !os.IsNotExist(err) {
						t.Fatalf("unavailable layout created an output file (stat error: %v)", err)
					}
				})
			}
		})
	}
}
