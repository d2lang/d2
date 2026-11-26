package e2etests_cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/d2lang/d2/internal/grammartest"
)

func TestGrammarFixtures(t *testing.T) {
	grammar := grammartest.Load(t)
	err := filepath.WalkDir("testdata", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".d2" {
			return err
		}
		t.Run(path, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			grammartest.Check(t, grammar, string(source))
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
