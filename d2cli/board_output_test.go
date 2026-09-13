package d2cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2target"
)

func TestBoardOutputComponent(t *testing.T) {
	for _, name := range []string{
		"board",
		"board name",
		"release.v2",
		"日本語",
		strings.Repeat("a", maxPortableBoardOutputComponentBytes),
		strings.Repeat("é", maxPortableBoardOutputComponentBytes/len("é")),
	} {
		if got := boardOutputComponent(name); got != name {
			t.Errorf("boardOutputComponent(%q) = %q, want unchanged", name, got)
		}
	}

	unsafeNames := []string{
		".",
		"..",
		"../escape",
		`..\escape`,
		"nested/board",
		`nested\board`,
		"/absolute/board",
		`\absolute\board`,
		`C:\absolute\board`,
		`C:drive-relative`,
		`\\server\share\board`,
		"NUL.txt",
		"NUL .txt",
		"CONIN$.txt",
		"CONOUT$",
		"COM¹.log",
		"LPT³",
		"index",
		"INDEX",
		"layers",
		"scenarios",
		"steps",
		boardOutputManifestName,
		string([]byte{0xff}),
		strings.Repeat("a", maxPortableBoardOutputComponentBytes+1),
		strings.Repeat("é", maxPortableBoardOutputComponentBytes/len("é")+1),
		escapedBoardOutputPrefix + "literal",
		"_D2_literal",
	}
	seen := make(map[string]string)
	for _, name := range unsafeNames {
		got := boardOutputComponent(name)
		if got != boardOutputComponent(name) {
			t.Fatalf("boardOutputComponent(%q) is not deterministic", name)
		}
		if got == name || !strings.HasPrefix(got, escapedBoardOutputPrefix) {
			t.Errorf("boardOutputComponent(%q) = %q, want escaped component", name, got)
		}
		if strings.ContainsAny(got, `/\`) || filepath.IsAbs(got) || got == "." || got == ".." {
			t.Errorf("boardOutputComponent(%q) = unsafe component %q", name, got)
		}
		if previous, ok := seen[got]; ok {
			t.Errorf("board names %q and %q map to the same component %q", previous, name, got)
		}
		seen[got] = name
	}
}

func TestValidateBoardOutputPathsRejectsCollisions(t *testing.T) {
	for _, test := range []struct {
		name   string
		first  string
		second string
	}{
		{name: "case insensitive filesystem", first: "Board", second: "board"},
		{name: "same escaped component", first: "../board", second: "../board"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagram := &d2target.Diagram{Layers: []*d2target.Diagram{
				{Name: test.first},
				{Name: test.second},
			}}
			err := validateBoardOutputPaths(filepath.Join(t.TempDir(), "output.svg"), diagram)
			if err == nil || !strings.Contains(err.Error(), "same output path") {
				t.Fatalf("validateBoardOutputPaths() error = %v, want collision", err)
			}
		})
	}
}

func TestValidateBoardOutputPathsRejectsFileDirectoryPrefixCollision(t *testing.T) {
	diagram := &d2target.Diagram{Layers: []*d2target.Diagram{
		{Name: "foo"},
		{Name: "foo.svg", IsFolderOnly: true, Layers: []*d2target.Diagram{{Name: "nested"}}},
	}}
	for _, outputPath := range []string{"output.svg", filepath.Join(t.TempDir(), "output.svg")} {
		err := validateBoardOutputPaths(outputPath, diagram)
		if err == nil || !strings.Contains(err.Error(), "requires a directory occupied") {
			t.Errorf("validateBoardOutputPaths(%q) error = %v, want file/directory collision", outputPath, err)
		}
	}
}

func TestAppendBoardOutputNameStaysContained(t *testing.T) {
	root := filepath.Join(t.TempDir(), "output")
	for _, name := range []string{
		"..",
		"../../escape",
		"nested/board",
		`nested\board`,
		"/absolute/board",
		`C:\absolute\board`,
		`\\server\share\board`,
	} {
		path, err := appendBoardOutputName(root+".svg", name)
		if err != nil {
			t.Fatalf("appendBoardOutputName(%q): %v", name, err)
		}
		path = strings.TrimSuffix(path, ".svg")
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("appendBoardOutputName(%q) produced escaping path %q", name, path)
		}
	}

	if err := ensurePathWithin(root, filepath.Join(root, "..", "escape")); err == nil {
		t.Fatal("ensurePathWithin accepted a parent traversal")
	}

	hiddenOutput := filepath.Join(t.TempDir(), ".svg")
	hiddenBoard, err := appendBoardOutputComponent(hiddenOutput, "index")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(hiddenOutput, "index") + ".svg"; hiddenBoard != want {
		t.Fatalf("hidden output board path = %q, want %q", hiddenBoard, want)
	}
}

func TestMultiboardOutputContainsUnsafeNames(t *testing.T) {
	directory := t.TempDir()
	workDirectory := filepath.Join(directory, "work")
	victimDirectory := filepath.Join(directory, "victim")
	if err := os.MkdirAll(workDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(victimDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(victimDirectory, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	inputPath := filepath.Join(workDirectory, "input.d2")
	if err := os.WriteFile(inputPath, []byte(`root
layers: {
  "../../victim": {
    child
    layers: {
      nested: {
        leaf
      }
    }
  }
  index: {
    structural-name
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(workDirectory, "output.svg")
	runBoardOutputCLI(t, workDirectory, inputPath, outputPath)

	if got, err := os.ReadFile(sentinelPath); err != nil || string(got) != "keep me" {
		t.Fatalf("outside sentinel = %q, %v; want unchanged", got, err)
	}
	escaped := boardOutputComponent("../../victim")
	for _, path := range []string{
		filepath.Join(workDirectory, "output", "index.svg"),
		filepath.Join(workDirectory, "output", escaped, "index.svg"),
		filepath.Join(workDirectory, "output", escaped, "nested.svg"),
		filepath.Join(workDirectory, "output", boardOutputComponent("index")+".svg"),
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Errorf("expected rendered board %q: %v", path, err)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(workDirectory, ".d2-board-output-*")); err != nil || len(matches) != 0 {
		t.Fatalf("staging directories after successful render = %v, %v", matches, err)
	}
}

func TestMultiboardOutputHashesOverlongBoardName(t *testing.T) {
	directory := t.TempDir()
	longName := strings.Repeat("a", 252)
	inputPath := filepath.Join(directory, "input.d2")
	input := "root\nlayers: {\n  \"" + longName + "\": { child }\n}\n"
	if err := os.WriteFile(inputPath, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(directory, "output.svg")
	runBoardOutputCLI(t, directory, inputPath, outputPath)

	escapedPath := filepath.Join(directory, "output", boardOutputComponent(longName)+".svg")
	if info, err := os.Stat(escapedPath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("overlong board was not rendered to its escaped path %q: %v", escapedPath, err)
	}
}

func TestMultiboardOutputPreservesPreexistingDirectory(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	if err := os.WriteFile(inputPath, []byte(`root
layers: {
  parent: {
    parent-shape
    layers: {
      child: {
        child-shape
      }
    }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(outputDirectory, "sentinel.txt")
	stalePath := filepath.Join(outputDirectory, "previous-board.svg")
	if err := os.WriteFile(sentinelPath, []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}

	runBoardOutputCLI(t, directory, inputPath, outputPath)
	// A second render exercises replacement of files generated by the first
	// render while the unrelated pre-existing files remain in place.
	runBoardOutputCLI(t, directory, inputPath, outputPath)

	for path, want := range map[string]string{sentinelPath: "unrelated", stalePath: "previous"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("pre-existing file %q = %q, %v; want %q", path, got, err, want)
		}
	}
	for _, path := range []string{
		filepath.Join(outputDirectory, "index.svg"),
		filepath.Join(outputDirectory, "parent", "index.svg"),
		filepath.Join(outputDirectory, "parent", "child.svg"),
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Errorf("expected rendered board %q: %v", path, err)
		}
	}
}

func TestMultiboardOutputRemovesOnlyUnmodifiedStaleGeneratedFiles(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	outputPath := filepath.Join(directory, "output.svg")
	first := `root
layers: {
  old: {
    layers: {
      leaf: { old-shape }
    }
  }
  edited: { edited-shape }
  keep: { keep-shape }
}
`
	if err := os.WriteFile(inputPath, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	runBoardOutputCLI(t, directory, inputPath, outputPath)

	outputDirectory := filepath.Join(directory, "output")
	editedPath := filepath.Join(outputDirectory, "edited.svg")
	if err := os.WriteFile(editedPath, []byte("user-edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(outputDirectory, "sentinel.txt")
	if err := os.WriteFile(sentinelPath, []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := `root
layers: {
  keep: { keep-shape }
}
`
	if err := os.WriteFile(inputPath, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	runBoardOutputCLI(t, directory, inputPath, outputPath)

	for _, path := range []string{
		filepath.Join(outputDirectory, "old", "index.svg"),
		filepath.Join(outputDirectory, "old", "leaf.svg"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale generated output %q still exists: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "old")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty stale generated directory still exists: %v", err)
	}
	for path, want := range map[string]string{editedPath: "user-edited", sentinelPath: "unrelated"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("preserved file %q = %q, %v; want %q", path, got, err, want)
		}
	}
	if info, err := os.Stat(filepath.Join(outputDirectory, boardOutputManifestName)); err != nil || !info.Mode().IsRegular() {
		t.Errorf("board output ownership manifest missing: %v", err)
	}
}

func TestMultiboardOutputHandlesEquivalentPathSpellingChanges(t *testing.T) {
	for _, test := range []struct {
		name                        string
		oldParent, oldChild         string
		currentParent, currentChild string
	}{
		{name: "case", oldParent: "Foo", oldChild: "Child", currentParent: "foo", currentChild: "child"},
		{name: "unicode normalization", oldParent: "é", oldChild: "É", currentParent: "e\u0301", currentChild: "E\u0301"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.d2")
			outputPath := filepath.Join(directory, "output.svg")
			first := "root\nlayers: {\n  \"" + test.oldParent + "\": {\n    layers: {\n      \"" + test.oldChild + "\": { old-shape }\n    }\n  }\n}\n"
			if err := os.WriteFile(inputPath, []byte(first), 0o644); err != nil {
				t.Fatal(err)
			}
			runBoardOutputCLI(t, directory, inputPath, outputPath)

			outputDirectory := filepath.Join(directory, "output")
			aliasesSameFile := sameBoardOutputPath(outputDirectory, test.oldParent, test.currentParent)
			second := "root\nlayers: {\n  \"" + test.currentParent + "\": {\n    layers: {\n      \"" + test.currentChild + "\": { new-shape }\n    }\n  }\n}\n"
			if err := os.WriteFile(inputPath, []byte(second), 0o644); err != nil {
				t.Fatal(err)
			}
			runBoardOutputCLI(t, directory, inputPath, outputPath)

			currentDirectory := filepath.Join(outputDirectory, test.currentParent)
			if info, err := os.Stat(filepath.Join(currentDirectory, test.currentChild+".svg")); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("new board output missing: %v", err)
			}
			oldDirectory := filepath.Join(outputDirectory, test.oldParent)
			if aliasesSameFile {
				oldInfo, oldErr := os.Stat(oldDirectory)
				currentInfo, currentErr := os.Stat(currentDirectory)
				if oldErr != nil || currentErr != nil || !os.SameFile(oldInfo, currentInfo) {
					t.Fatalf("filesystem aliases diverged: old=%v/%v current=%v/%v", oldInfo, oldErr, currentInfo, currentErr)
				}
			} else if _, err := os.Stat(oldDirectory); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale board directory remains on spelling-sensitive filesystem: %v", err)
			}

			manifest, _, err := readBoardOutputManifest(outputDirectory)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range manifest.Files {
				if strings.Contains(file.Path, test.oldParent) || strings.Contains(file.Path, test.oldChild) {
					t.Errorf("manifest retained stale spelling %q", file.Path)
				}
			}
			for _, dir := range manifest.Directories {
				if strings.Contains(dir, test.oldParent) || strings.Contains(dir, test.oldChild) {
					t.Errorf("manifest retained stale spelling %q", dir)
				}
			}
		})
	}
}

func TestBoardOutputPublicationRollsBackWholeTransaction(t *testing.T) {
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	staleDirectory := filepath.Join(outputDirectory, "stale")
	if err := os.MkdirAll(staleDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	replacedPath := filepath.Join(outputDirectory, "replaced.svg")
	stalePath := filepath.Join(staleDirectory, "old.svg")
	if err := os.WriteFile(replacedPath, []byte("old replacement"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte("old stale output"), 0o644); err != nil {
		t.Fatal(err)
	}
	replacedDigest, err := boardOutputFileDigest(replacedPath)
	if err != nil {
		t.Fatal(err)
	}
	staleDigest, err := boardOutputFileDigest(stalePath)
	if err != nil {
		t.Fatal(err)
	}
	previous := boardOutputManifest{
		Format: boardOutputManifestFormat, Version: boardOutputManifestVersion,
		Files: []boardOutputManifestFile{
			{Path: "replaced.svg", SHA256: replacedDigest},
			{Path: "stale/old.svg", SHA256: staleDigest},
		},
		Directories: []string{"stale"},
	}
	manifestPath := filepath.Join(outputDirectory, boardOutputManifestName)
	if err := writeBoardOutputManifest(manifestPath, previous); err != nil {
		t.Fatal(err)
	}
	originalManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := newBoardOutputWorkspace(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.stageRoot, "created.svg"), []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.stageRoot, "replaced.svg"), []byte("new replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected publication failure")
	workspace.beforePublish = func(path string) error {
		if filepath.Base(path) == boardOutputManifestName {
			return injected
		}
		return nil
	}
	touched, err := workspace.publish()
	if !errors.Is(err, injected) {
		t.Fatalf("publish() error = %v, want injected failure", err)
	}
	if touched {
		t.Fatal("publish() reported output touched after a successful rollback")
	}

	for path, want := range map[string]string{
		replacedPath: "old replacement",
		stalePath:    "old stale output",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("rolled-back file %q = %q, %v; want %q", path, got, err, want)
		}
	}
	if info, err := os.Stat(replacedPath); err != nil {
		t.Errorf("inspect replacement mode after rollback: %v", err)
	} else if info.Mode().Perm() != 0o640 {
		t.Errorf("replacement mode after rollback = %v; want 0640", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "created.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("new file survived rollback: %v", err)
	}
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != string(originalManifest) {
		t.Errorf("manifest after rollback = %q, %v; want original", got, err)
	}
	if matches, err := filepath.Glob(filepath.Join(directory, ".d2-board-output-*")); err != nil || len(matches) != 0 {
		t.Errorf("staging directories after rollback = %v, %v", matches, err)
	}
}

func TestBoardOutputPublicationRejectsAncestorSwapWithoutEscapingRoot(t *testing.T) {
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	nestedDirectory := filepath.Join(outputDirectory, "nested")
	movedDirectory := filepath.Join(directory, "moved-nested")
	victimDirectory := filepath.Join(directory, "victim")
	if err := os.MkdirAll(nestedDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(victimDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDirectory, "sentinel"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(directory, "symlink-probe")
	if err := os.Symlink(victimDirectory, probe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	workspace, err := newBoardOutputWorkspace(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace.stageRoot, "a-created"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace.stageRoot, "nested", "leaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.stageRoot, "nested", "leaf", "board.svg"), []byte("new board"), 0o644); err != nil {
		t.Fatal(err)
	}

	swapped := false
	workspace.beforePublish = func(path string) error {
		if path != filepath.Join(nestedDirectory, "leaf") {
			return nil
		}
		swapped = true
		if err := os.Rename(nestedDirectory, movedDirectory); err != nil {
			return err
		}
		return os.Symlink(victimDirectory, nestedDirectory)
	}
	touched, err := workspace.publish()
	if err == nil {
		t.Fatal("publish() accepted a symlink ancestor installed during publication")
	}
	if touched {
		t.Fatal("publish() reported output touched after rolling back the transaction")
	}
	if !swapped {
		t.Fatal("publication did not reach the injected ancestor swap")
	}
	if _, err := os.Stat(filepath.Join(victimDirectory, "leaf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publication created a directory outside its root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(victimDirectory, "leaf", "board.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("publication created a file outside its root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "a-created")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transaction directory survived rollback: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(movedDirectory, "sentinel")); err != nil || string(got) != "keep me" {
		t.Fatalf("preexisting nested output = %q, %v; want unchanged", got, err)
	}
	if matches, err := filepath.Glob(filepath.Join(directory, ".d2-board-output-*")); err != nil || len(matches) != 0 {
		t.Fatalf("staging directories after rollback = %v, %v", matches, err)
	}
}

func TestBoardOutputPublicationPreservesStaleFileEditedDuringPublish(t *testing.T) {
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(outputDirectory, "stale.svg")
	if err := os.WriteFile(stalePath, []byte("generated"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := boardOutputFileDigest(stalePath)
	if err != nil {
		t.Fatal(err)
	}
	previous := boardOutputManifest{
		Format: boardOutputManifestFormat, Version: boardOutputManifestVersion,
		Files: []boardOutputManifestFile{{Path: "stale.svg", SHA256: digest}},
	}
	if err := writeBoardOutputManifest(filepath.Join(outputDirectory, boardOutputManifestName), previous); err != nil {
		t.Fatal(err)
	}

	workspace, err := newBoardOutputWorkspace(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.stageRoot, "current.svg"), []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := false
	workspace.beforePublish = func(path string) error {
		if path == stalePath && !edited {
			edited = true
			return os.WriteFile(stalePath, []byte("edited during publication"), 0o644)
		}
		return nil
	}
	if touched, err := workspace.publish(); err != nil || !touched {
		t.Fatalf("publish() = %v, %v; want successful publication", touched, err)
	}
	if got, err := os.ReadFile(stalePath); err != nil || string(got) != "edited during publication" {
		t.Fatalf("concurrently edited stale output = %q, %v; want preserved", got, err)
	}
	manifest, _, err := readBoardOutputManifest(outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range manifest.Files {
		if file.Path == "stale.svg" {
			t.Fatal("new manifest retained ownership of a user-edited stale file")
		}
	}
}

func TestValidateBoardOutputManifestRejectsUnsafeOwnershipClaims(t *testing.T) {
	digest := strings.Repeat("0", 2*32)
	for _, test := range []struct {
		name     string
		manifest boardOutputManifest
	}{
		{
			name: "case alias of manifest",
			manifest: boardOutputManifest{Files: []boardOutputManifestFile{
				{Path: strings.ToUpper(boardOutputManifestName), SHA256: digest},
			}},
		},
		{
			name: "file prefix of file",
			manifest: boardOutputManifest{Files: []boardOutputManifestFile{
				{Path: "file", SHA256: digest},
				{Path: "file/child.svg", SHA256: digest},
			}},
		},
		{
			name: "file prefix of directory",
			manifest: boardOutputManifest{
				Files:       []boardOutputManifestFile{{Path: "file", SHA256: digest}},
				Directories: []string{"file/child"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.manifest.Format = boardOutputManifestFormat
			test.manifest.Version = boardOutputManifestVersion
			if err := validateBoardOutputManifest(t.TempDir(), test.manifest); err == nil {
				t.Fatal("validateBoardOutputManifest() accepted unsafe ownership claims")
			}
		})
	}
}

func TestMultiboardOutputRejectsPreexistingSymlink(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	if err := os.WriteFile(inputPath, []byte(`root
layers: {
  child: {
    child-shape
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	victimPath := filepath.Join(directory, "victim.svg")
	if err := os.WriteFile(victimPath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimPath, filepath.Join(outputDirectory, "child.svg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := runBoardOutputCLIResult(t, directory, inputPath, outputPath)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink rejection", err)
	}
	if got, err := os.ReadFile(victimPath); err != nil || string(got) != "keep me" {
		t.Fatalf("symlink target = %q, %v; want unchanged", got, err)
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "index.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root output was published before symlink preflight completed: %v", err)
	}
}

func TestMultiboardOutputRejectsPreexistingSymlinkAncestor(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	if err := os.WriteFile(inputPath, []byte(`root
layers: {
  parent: {
    layers: {
      child: { child-shape }
    }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(directory, "output.svg")
	outputDirectory := filepath.Join(directory, "output")
	victimDirectory := filepath.Join(directory, "victim")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(victimDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(victimDirectory, "sentinel")
	if err := os.WriteFile(sentinelPath, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimDirectory, filepath.Join(outputDirectory, "parent")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := runBoardOutputCLIResult(t, directory, inputPath, outputPath)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Run() error = %v, want symlink ancestor rejection", err)
	}
	if got, err := os.ReadFile(sentinelPath); err != nil || string(got) != "keep me" {
		t.Fatalf("victim sentinel = %q, %v; want unchanged", got, err)
	}
	if _, err := os.Stat(filepath.Join(victimDirectory, "child.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("board was published through symlink ancestor: %v", err)
	}
}

func TestRelinkIncludesConnections(t *testing.T) {
	diagram := &d2target.Diagram{
		Shapes:      []d2target.Shape{{Link: "root.layers.child"}},
		Connections: []d2target.Connection{{Link: "root.layers.child"}},
	}
	links := map[string]string{
		"root":              filepath.Join("output", "index.svg"),
		"root.layers.child": filepath.Join("output", "child #%.svg"),
	}
	if err := relink("root", diagram, links); err != nil {
		t.Fatal(err)
	}
	for kind, link := range map[string]string{
		"shape":      diagram.Shapes[0].Link,
		"connection": diagram.Connections[0].Link,
	} {
		if link != "child%20%23%25.svg" {
			t.Errorf("%s board link = %q, want URL-escaped child path", kind, link)
		}
		if strings.Contains(link, `\`) {
			t.Errorf("%s board link %q is not URL-style", kind, link)
		}
	}
	if got := boardOutputLink(filepath.Join("..", "child #%.svg")); got != "../child%20%23%25.svg" {
		t.Errorf("parent-relative board link = %q, want URL-style escaped path", got)
	}
}

func runBoardOutputCLI(t *testing.T, directory string, inputPath, outputPath string) {
	t.Helper()
	if err := runBoardOutputCLIResult(t, directory, inputPath, outputPath); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
}

func runBoardOutputCLIResult(t *testing.T, directory string, inputPath, outputPath string) error {
	t.Helper()
	state := &xmain.TestState{
		Run:  Run,
		Args: []string{"d2", inputPath, outputPath},
		PWD:  directory,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	return state.Wait(ctx)
}
