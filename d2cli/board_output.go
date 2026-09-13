package d2cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/d2lang/d2/d2target"
)

const (
	escapedBoardOutputPrefix             = "_d2_"
	maxPortableBoardOutputComponentBytes = 200
	boardOutputManifestName              = ".d2-board-output-manifest-v1.json"
)

// boardOutputComponent preserves ordinary board names while mapping names that
// have filesystem meaning to a portable, single path component. The prefix is
// reserved so a literal board name cannot collide with an escaped name.
func boardOutputComponent(name string) string {
	if isPortableBoardOutputComponent(name) && !isReservedBoardOutputComponent(name) {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return escapedBoardOutputPrefix + hex.EncodeToString(sum[:])
}

func isReservedBoardOutputComponent(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, escapedBoardOutputPrefix) {
		return true
	}
	switch lower {
	case "index", "layers", "scenarios", "steps", boardOutputManifestName:
		return true
	default:
		return false
	}
}

func isPortableBoardOutputComponent(name string) bool {
	if name == "" || !utf8.ValidString(name) || len(name) > maxPortableBoardOutputComponentBytes || name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	if !filepath.IsLocal(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			return false
		}
	}

	// Windows reserves these names even when they have an extension.
	base := name
	if i := strings.IndexAny(base, ".:"); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, " ")
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
		"COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³", "CONIN$", "CONOUT$":
		return false
	}
	return true
}

func appendBoardOutputName(outputPath, boardName string) (string, error) {
	return appendBoardOutputComponent(outputPath, boardOutputComponent(boardName))
}

func appendBoardOutputComponent(outputPath, component string) (string, error) {
	outputDir, ext := splitBoardOutputPath(outputPath)
	joined := filepath.Join(outputDir, component)
	if err := ensurePathWithin(outputDir, joined); err != nil {
		return "", fmt.Errorf("invalid board output component %q: %w", component, err)
	}
	return joined + ext, nil
}

func splitBoardOutputPath(outputPath string) (root, ext string) {
	ext = filepath.Ext(outputPath)
	if strings.TrimSuffix(filepath.Base(outputPath), ext) == "" {
		return outputPath, ext
	}
	return strings.TrimSuffix(outputPath, ext), ext
}

func ensurePathWithin(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return err
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes root %q", path, root)
	}
	return nil
}

type boardOutputPaths struct {
	writePath   string
	displayPath string
}

type boardOutputPlan struct {
	base      boardOutputPaths
	board     boardOutputPaths
	layers    boardOutputPaths
	scenarios boardOutputPaths
	steps     boardOutputPaths
}

func newBoardOutputPaths(path string) boardOutputPaths {
	return boardOutputPaths{writePath: path, displayPath: path}
}

func (p boardOutputPaths) withBoardName(name string) (boardOutputPaths, error) {
	writePath, err := appendBoardOutputName(p.writePath, name)
	if err != nil {
		return boardOutputPaths{}, err
	}
	displayPath, err := appendBoardOutputName(p.displayPath, name)
	if err != nil {
		return boardOutputPaths{}, err
	}
	return boardOutputPaths{writePath: writePath, displayPath: displayPath}, nil
}

func (p boardOutputPaths) withComponent(component string) (boardOutputPaths, error) {
	writePath, err := appendBoardOutputComponent(p.writePath, component)
	if err != nil {
		return boardOutputPaths{}, err
	}
	displayPath, err := appendBoardOutputComponent(p.displayPath, component)
	if err != nil {
		return boardOutputPaths{}, err
	}
	return boardOutputPaths{writePath: writePath, displayPath: displayPath}, nil
}

func planBoardOutput(paths boardOutputPaths, diagram *d2target.Diagram) (boardOutputPlan, error) {
	var err error
	if diagram.Name != "" {
		paths, err = paths.withBoardName(diagram.Name)
		if err != nil {
			return boardOutputPlan{}, err
		}
	}

	plan := boardOutputPlan{
		base:      paths,
		board:     paths,
		layers:    paths,
		scenarios: paths,
		steps:     paths,
	}
	if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0 {
		plan.board, err = plan.board.withComponent("index")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	if len(diagram.Scenarios) > 0 || len(diagram.Steps) > 0 {
		plan.layers, err = plan.layers.withComponent("layers")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	if len(diagram.Layers) > 0 || len(diagram.Steps) > 0 {
		plan.scenarios, err = plan.scenarios.withComponent("scenarios")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	if len(diagram.Layers) > 0 || len(diagram.Scenarios) > 0 {
		plan.steps, err = plan.steps.withComponent("steps")
		if err != nil {
			return boardOutputPlan{}, err
		}
	}
	return plan, nil
}

func validateBoardOutputPaths(outputPath string, diagram *d2target.Diagram) error {
	claims := &boardOutputClaims{
		files: make(map[string]boardOutputClaim),
		dirs:  make(map[string]boardOutputClaim),
	}
	return validateBoardOutputPathsRecursive("root", newBoardOutputPaths(outputPath), diagram, claims)
}

type boardOutputClaim struct {
	diagramPath string
	path        string
}

type boardOutputClaims struct {
	files map[string]boardOutputClaim
	dirs  map[string]boardOutputClaim
}

func (c *boardOutputClaims) addFile(diagramPath, path string) error {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	key := boardOutputCollisionKey(absolutePath)
	if previous, ok := c.files[key]; ok {
		return fmt.Errorf("boards %q and %q resolve to the same output path %q", previous.diagramPath, diagramPath, path)
	}
	if previous, ok := c.dirs[key]; ok {
		return fmt.Errorf("board %q output file %q collides with a directory required by board %q", diagramPath, path, previous.diagramPath)
	}

	dir := filepath.Dir(absolutePath)
	for {
		dirKey := boardOutputCollisionKey(dir)
		if previous, ok := c.files[dirKey]; ok {
			return fmt.Errorf("board %q output file %q requires a directory occupied by board %q output %q", diagramPath, path, previous.diagramPath, previous.path)
		}
		if _, ok := c.dirs[dirKey]; !ok {
			c.dirs[dirKey] = boardOutputClaim{diagramPath: diagramPath, path: dir}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	c.files[key] = boardOutputClaim{diagramPath: diagramPath, path: path}
	return nil
}

func validateBoardOutputPathsRecursive(diagramPath string, outputPaths boardOutputPaths, diagram *d2target.Diagram, claims *boardOutputClaims) error {
	plan, err := planBoardOutput(outputPaths, diagram)
	if err != nil {
		return err
	}
	if !diagram.IsFolderOnly {
		if err := claims.addFile(diagramPath, plan.board.displayPath); err != nil {
			return err
		}
	}
	for _, child := range diagram.Layers {
		if err := validateBoardOutputPathsRecursive(diagramPath+".layers."+child.Name, plan.layers, child, claims); err != nil {
			return err
		}
	}
	for _, child := range diagram.Scenarios {
		if err := validateBoardOutputPathsRecursive(diagramPath+".scenarios."+child.Name, plan.scenarios, child, claims); err != nil {
			return err
		}
	}
	for _, child := range diagram.Steps {
		if err := validateBoardOutputPathsRecursive(diagramPath+".steps."+child.Name, plan.steps, child, claims); err != nil {
			return err
		}
	}
	return nil
}

func boardOutputCollisionKey(path string) string {
	return norm.NFC.String(cases.Fold().String(filepath.Clean(path)))
}

// boardOutputWorkspace keeps all content-derived paths inside a private
// directory created by this process. Publishing transactionally replaces the
// current generated files, removes only unchanged stale generated files, and
// preserves unrelated files in an existing output tree.
type boardOutputWorkspace struct {
	finalRoot string
	stageRoot string
	stageInfo fs.FileInfo
	extension string
	// beforePublish is set only by tests to inject a deterministic failure at
	// a specific visible mutation boundary.
	beforePublish func(string) error
}

func newBoardOutputWorkspace(outputPath string) (*boardOutputWorkspace, error) {
	finalRoot, ext := splitBoardOutputPath(outputPath)
	if err := validateBoardOutputRoot(finalRoot); err != nil {
		return nil, err
	}
	parent := filepath.Dir(finalRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create board output parent: %w", err)
	}
	stageRoot, err := os.MkdirTemp(parent, ".d2-board-output-")
	if err != nil {
		return nil, fmt.Errorf("create board output staging directory: %w", err)
	}
	stageInfo, err := os.Lstat(stageRoot)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("inspect board output staging directory: %w", err),
			func() error {
				if cleanupErr := os.Remove(stageRoot); cleanupErr != nil {
					return fmt.Errorf("remove board output staging directory %q: %w", stageRoot, cleanupErr)
				}
				return nil
			}(),
		)
	}
	return &boardOutputWorkspace{finalRoot: finalRoot, stageRoot: stageRoot, stageInfo: stageInfo, extension: ext}, nil
}

func validateBoardOutputRoot(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect board output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("board output root %q must be a real directory", path)
	}
	return nil
}

func (w *boardOutputWorkspace) outputPaths(displayPath string) boardOutputPaths {
	return boardOutputPaths{
		writePath:   w.stageRoot + w.extension,
		displayPath: displayPath,
	}
}

func (w *boardOutputWorkspace) discard() error {
	if w.stageRoot == "" {
		return nil
	}
	stageRoot := w.stageRoot
	stageInfo, err := os.Lstat(stageRoot)
	if errors.Is(err, os.ErrNotExist) {
		w.stageRoot = ""
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect board output staging directory %q: %w", stageRoot, err)
	}
	if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() || !os.SameFile(w.stageInfo, stageInfo) {
		return fmt.Errorf("refusing to remove replaced board output staging path %q", stageRoot)
	}
	stage, err := os.OpenRoot(stageRoot)
	if err != nil {
		return fmt.Errorf("open board output staging directory %q: %w", stageRoot, err)
	}
	openedInfo, err := stage.Stat(".")
	if err != nil || !os.SameFile(w.stageInfo, openedInfo) {
		return errors.Join(
			fmt.Errorf("board output staging path %q changed while opening it", stageRoot),
			err,
			stage.Close(),
		)
	}
	directory, err := stage.Open(".")
	if err != nil {
		return errors.Join(fmt.Errorf("open board output staging entries: %w", err), stage.Close())
	}
	entries, err := directory.ReadDir(-1)
	if closeErr := directory.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	if err != nil {
		return errors.Join(fmt.Errorf("read board output staging entries: %w", err), stage.Close())
	}
	for _, entry := range entries {
		if err := stage.RemoveAll(entry.Name()); err != nil {
			return errors.Join(
				fmt.Errorf("remove board output staging entry %q: %w", entry.Name(), err),
				stage.Close(),
			)
		}
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close board output staging directory %q: %w", stageRoot, err)
	}
	stageInfo, err = os.Lstat(stageRoot)
	if err != nil {
		return fmt.Errorf("inspect emptied board output staging directory %q: %w", stageRoot, err)
	}
	if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() || !os.SameFile(w.stageInfo, stageInfo) {
		return fmt.Errorf("refusing to remove replaced board output staging path %q", stageRoot)
	}
	if err := os.Remove(stageRoot); err != nil {
		return fmt.Errorf("remove emptied board output staging directory %q: %w", stageRoot, err)
	}
	w.stageRoot = ""
	return nil
}

func (w *boardOutputWorkspace) validateStageRoot() error {
	stageInfo, err := os.Lstat(w.stageRoot)
	if err != nil {
		return fmt.Errorf("inspect board output staging directory %q: %w", w.stageRoot, err)
	}
	if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() || !os.SameFile(w.stageInfo, stageInfo) {
		return fmt.Errorf("board output staging path %q was replaced", w.stageRoot)
	}
	return nil
}

type stagedBoardOutput struct {
	rel  string
	mode fs.FileMode
	dir  bool
}

func (w *boardOutputWorkspace) publish() (touched bool, err error) {
	return publishBoardOutputWorkspace(w)
}

func (w *boardOutputWorkspace) preflightMerge(final *boardOutputRoot) ([]stagedBoardOutput, error) {
	var entries []stagedBoardOutput
	err := filepath.WalkDir(w.stageRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(w.stageRoot, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(w.finalRoot, rel)
		if err := ensurePathWithin(w.finalRoot, destination); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("staged board output %q is not a regular file or directory", path)
		}

		if final == nil {
			entries = append(entries, stagedBoardOutput{rel: rel, mode: info.Mode(), dir: info.IsDir()})
			return nil
		}
		destinationInfo, err := final.handle.Lstat(rel)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			if destinationInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to publish board output through symlink %q", destination)
			}
			if info.IsDir() != destinationInfo.IsDir() || (!info.IsDir() && !destinationInfo.Mode().IsRegular()) {
				return fmt.Errorf("board output path %q has an incompatible existing type", destination)
			}
		}
		entries = append(entries, stagedBoardOutput{rel: rel, mode: info.Mode(), dir: info.IsDir()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("preflight board output tree: %w", err)
	}
	return entries, nil
}
