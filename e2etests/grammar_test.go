package e2etests

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/d2lang/d2/internal/grammartest"
)

func TestGrammarFixtures(t *testing.T) {
	grammar := grammartest.Load(t)
	check := func(t *testing.T, tc testCase) {
		grammartest.Check(t, grammar, tc.script)
	}

	// Collect all registered sources even when -run selects a single subtest.
	registered := make(map[string]bool)
	for _, group := range fixtureGroups {
		cases := group.cases(t)
		for _, tc := range cases {
			registered[tc.script] = true
		}
		t.Run(group.name, func(t *testing.T) { runFixtureCases(t, cases, check) })
	}

	root := filepath.Join("testdata", "files")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".d2" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(data)
		if !registered[source] {
			t.Run(filepath.ToSlash(path), func(t *testing.T) { grammartest.Check(t, grammar, source) })
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
