package d2cli

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	boardOutputManifestFormat   = "d2-board-output"
	boardOutputManifestVersion  = 1
	maxBoardOutputManifestBytes = 16 << 20
	maxBoardOutputManifestPaths = 100_000
)

type boardOutputManifest struct {
	Format      string                    `json:"format"`
	Version     int                       `json:"version"`
	Files       []boardOutputManifestFile `json:"files"`
	Directories []string                  `json:"directories"`
}

type boardOutputManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func publishBoardOutputWorkspace(w *boardOutputWorkspace) (touched bool, err error) {
	defer func() {
		if cleanupErr := w.discard(); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	if err := w.validateStageRoot(); err != nil {
		return false, err
	}
	if err := validateBoardOutputRoot(w.finalRoot); err != nil {
		return false, err
	}
	final, err := openExistingBoardOutputRoot(w.finalRoot)
	if err != nil {
		return false, err
	}
	if final != nil {
		defer func() { err = errors.Join(err, final.close()) }()
	}
	entries, err := w.preflightMerge(final)
	if err != nil {
		return false, err
	}
	manifest, err := w.buildManifest(entries)
	if err != nil {
		return false, err
	}
	manifestSource := filepath.Join(w.stageRoot, boardOutputManifestName)
	if err := writeBoardOutputManifest(manifestSource, manifest); err != nil {
		return false, err
	}
	if err := w.validateStageRoot(); err != nil {
		return false, err
	}

	if final == nil {
		_, statErr := os.Lstat(w.finalRoot)
		if statErr == nil {
			final, err = openBoardOutputRoot(w.finalRoot)
			if err != nil {
				return false, err
			}
			defer func() { err = errors.Join(err, final.close()) }()
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return false, fmt.Errorf("inspect board output root: %w", statErr)
		}
	}
	if final == nil {
		if w.beforePublish != nil {
			if err := w.beforePublish(w.finalRoot); err != nil {
				return false, err
			}
		}
		if err := os.Chmod(w.stageRoot, 0o755); err != nil {
			return false, fmt.Errorf("set published board output directory permissions: %w", err)
		}
		if err := w.validateStageRoot(); err != nil {
			return false, err
		}
		if err := os.Rename(w.stageRoot, w.finalRoot); err != nil {
			return false, fmt.Errorf("publish board output tree: %w", err)
		}
		w.stageRoot = ""
		return true, nil
	}

	previous, _, err := readBoardOutputManifestRoot(final)
	if err != nil {
		return false, err
	}
	transaction := boardOutputTransaction{workspace: w, root: final}
	if err := transaction.apply(entries, previous, manifest, manifestSource); err != nil {
		rollbackErr := transaction.rollback()
		return rollbackErr != nil, errors.Join(err, rollbackErr)
	}
	return transaction.touched(), nil
}

// boardOutputRoot keeps publication anchored to the directory that was
// verified when it was opened. On supported platforms, os.Root resolves every
// later operation relative to that handle and rejects symlinks that escape it.
// os.Root documents weaker rename and symlink guarantees on plan9 and js; D2's
// native CLI targets use the descriptor/handle-backed implementations.
type boardOutputRoot struct {
	path   string
	handle *os.Root
}

func openExistingBoardOutputRoot(path string) (*boardOutputRoot, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect board output root: %w", err)
	}
	return openBoardOutputRoot(path)
}

func openBoardOutputRoot(path string) (*boardOutputRoot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect board output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("board output root %q must be a real directory", path)
	}
	handle, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open board output root %q: %w", path, err)
	}
	openedInfo, statErr := handle.Stat(".")
	if statErr != nil || !os.SameFile(info, openedInfo) {
		return nil, errors.Join(
			fmt.Errorf("board output root %q changed while opening it", path),
			statErr,
			handle.Close(),
		)
	}
	return &boardOutputRoot{path: path, handle: handle}, nil
}

func (r *boardOutputRoot) close() error {
	if err := r.handle.Close(); err != nil {
		return fmt.Errorf("close board output root %q: %w", r.path, err)
	}
	return nil
}

func (r *boardOutputRoot) display(rel string) string {
	return filepath.Join(r.path, rel)
}

func (w *boardOutputWorkspace) buildManifest(entries []stagedBoardOutput) (boardOutputManifest, error) {
	manifest := boardOutputManifest{
		Format: boardOutputManifestFormat, Version: boardOutputManifestVersion,
		Files: []boardOutputManifestFile{}, Directories: []string{},
	}
	for _, entry := range entries {
		if entry.rel == "." {
			continue
		}
		rel := filepath.ToSlash(entry.rel)
		if entry.dir {
			manifest.Directories = append(manifest.Directories, rel)
			continue
		}
		digest, err := boardOutputFileDigest(filepath.Join(w.stageRoot, entry.rel))
		if err != nil {
			return boardOutputManifest{}, fmt.Errorf("hash staged board output %q: %w", entry.rel, err)
		}
		manifest.Files = append(manifest.Files, boardOutputManifestFile{Path: rel, SHA256: digest})
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	sort.Strings(manifest.Directories)
	if err := validateBoardOutputManifest(w.finalRoot, manifest); err != nil {
		return boardOutputManifest{}, fmt.Errorf("validate generated board output manifest: %w", err)
	}
	return manifest, nil
}

func writeBoardOutputManifest(path string, manifest boardOutputManifest) (err error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode board output manifest: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxBoardOutputManifestBytes {
		return fmt.Errorf("board output manifest exceeds %d bytes", maxBoardOutputManifestBytes)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create board output manifest: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write board output manifest: %w", err)
	}
	return nil
}

func readBoardOutputManifest(root string) (manifest boardOutputManifest, exists bool, err error) {
	final, err := openExistingBoardOutputRoot(root)
	if err != nil || final == nil {
		return boardOutputManifest{}, false, err
	}
	defer func() { err = errors.Join(err, final.close()) }()
	return readBoardOutputManifestRoot(final)
}

func readBoardOutputManifestRoot(root *boardOutputRoot) (boardOutputManifest, bool, error) {
	path := root.display(boardOutputManifestName)
	info, err := root.handle.Lstat(boardOutputManifestName)
	if errors.Is(err, os.ErrNotExist) {
		return boardOutputManifest{}, false, nil
	}
	if err != nil {
		return boardOutputManifest{}, false, fmt.Errorf("inspect board output manifest: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return boardOutputManifest{}, false, fmt.Errorf("board output manifest %q is not a regular file", path)
	}
	if info.Size() > maxBoardOutputManifestBytes {
		return boardOutputManifest{}, false, fmt.Errorf("board output manifest exceeds %d bytes", maxBoardOutputManifestBytes)
	}
	file, err := openBoardOutputRootRegularFile(root, boardOutputManifestName, info)
	if err != nil {
		return boardOutputManifest{}, false, fmt.Errorf("open board output manifest: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBoardOutputManifestBytes+1))
	if err != nil {
		return boardOutputManifest{}, false, fmt.Errorf("read board output manifest: %w", err)
	}
	if len(data) > maxBoardOutputManifestBytes {
		return boardOutputManifest{}, false, fmt.Errorf("board output manifest exceeds %d bytes", maxBoardOutputManifestBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest boardOutputManifest
	if err := decoder.Decode(&manifest); err != nil {
		return boardOutputManifest{}, false, fmt.Errorf("decode board output manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return boardOutputManifest{}, false, fmt.Errorf("decode board output manifest: trailing data")
	}
	if err := validateBoardOutputManifest(root.path, manifest); err != nil {
		return boardOutputManifest{}, false, err
	}
	return manifest, true, nil
}

func validateBoardOutputManifest(root string, manifest boardOutputManifest) error {
	if manifest.Format != boardOutputManifestFormat || manifest.Version != boardOutputManifestVersion {
		return fmt.Errorf("unsupported board output manifest format or version")
	}
	if len(manifest.Files)+len(manifest.Directories) > maxBoardOutputManifestPaths {
		return fmt.Errorf("board output manifest exceeds %d paths", maxBoardOutputManifestPaths)
	}
	seen := make(map[string]string, len(manifest.Files)+len(manifest.Directories))
	pathKinds := make(map[string]bool, len(manifest.Files)+len(manifest.Directories)) // true for files
	validatePath := func(path string, file bool) error {
		native := filepath.FromSlash(path)
		if path == "" || path == "." || strings.Contains(path, `\`) || filepath.ToSlash(filepath.Clean(native)) != path || !filepath.IsLocal(native) || filepath.IsAbs(native) || filepath.VolumeName(native) != "" {
			return fmt.Errorf("board output manifest contains invalid path %q", path)
		}
		if err := ensurePathWithin(root, filepath.Join(root, native)); err != nil {
			return fmt.Errorf("board output manifest contains invalid path %q: %w", path, err)
		}
		key := boardOutputCollisionKey(native)
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("board output manifest paths %q and %q collide", previous, path)
		}
		if key == boardOutputCollisionKey(boardOutputManifestName) {
			return fmt.Errorf("board output manifest cannot own itself")
		}
		seen[key] = path
		pathKinds[key] = file
		return nil
	}
	for _, file := range manifest.Files {
		if err := validatePath(file.Path, true); err != nil {
			return err
		}
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("board output manifest contains invalid digest for %q", file.Path)
		}
	}
	for _, dir := range manifest.Directories {
		if err := validatePath(dir, false); err != nil {
			return err
		}
	}
	for _, path := range seen {
		for ancestor := filepath.Dir(filepath.FromSlash(path)); ancestor != "."; ancestor = filepath.Dir(ancestor) {
			if pathKinds[boardOutputCollisionKey(ancestor)] {
				return fmt.Errorf("board output manifest path %q is beneath a file path", path)
			}
		}
	}
	return nil
}

func boardOutputFileDigest(path string) (digest string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func boardOutputRootFileDigest(root *boardOutputRoot, rel string, expected fs.FileInfo) (digest string, err error) {
	file, err := openBoardOutputRootRegularFile(root, rel, expected)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func openBoardOutputRootRegularFile(root *boardOutputRoot, rel string, expected fs.FileInfo) (*os.File, error) {
	file, err := root.handle.Open(rel)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || expected != nil && !os.SameFile(expected, info) {
		if statErr == nil {
			if !info.Mode().IsRegular() {
				statErr = fmt.Errorf("not a regular file")
			} else {
				statErr = fmt.Errorf("file changed while opening it")
			}
		}
		return nil, errors.Join(
			fmt.Errorf("open board output %q: %w", root.display(rel), statErr),
			file.Close(),
		)
	}
	return file, nil
}

func copyBoardOutputRootFile(root *boardOutputRoot, sourceRel, destination string, sourceInfo fs.FileInfo, mode fs.FileMode, exclusive bool) (err error) {
	input, err := openBoardOutputRootRegularFile(root, sourceRel, sourceInfo)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags |= os.O_EXCL
	}
	output, err := os.OpenFile(destination, flags, mode)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, output.Close()) }()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	return output.Chmod(mode)
}

type boardOutputFileChange struct {
	rel            string
	source         string
	remove         bool
	existed        bool
	originalInfo   fs.FileInfo
	originalMode   fs.FileMode
	backup         string
	expectedSHA256 string
	skip           bool
	applied        bool
	installedInfo  fs.FileInfo
}

type boardOutputRemovedDirectory struct {
	rel  string
	mode fs.FileMode
}

type boardOutputTransaction struct {
	workspace       *boardOutputWorkspace
	root            *boardOutputRoot
	changes         []*boardOutputFileChange
	appliedChanges  []*boardOutputFileChange
	createdDirs     []string
	removedDirs     []boardOutputRemovedDirectory
	visibleMutation bool
}

func (t *boardOutputTransaction) touched() bool {
	return t.visibleMutation
}

func (t *boardOutputTransaction) apply(entries []stagedBoardOutput, previous, current boardOutputManifest, manifestSource string) error {
	currentFiles := make(map[string]string, len(current.Files))
	for _, file := range current.Files {
		currentFiles[boardOutputCollisionKey(filepath.FromSlash(file.Path))] = filepath.FromSlash(file.Path)
	}
	for _, entry := range entries {
		if entry.dir {
			continue
		}
		t.changes = append(t.changes, &boardOutputFileChange{
			rel: entry.rel, source: filepath.Join(t.workspace.stageRoot, entry.rel),
		})
	}
	staleChanges, err := t.staleFileChanges(previous, currentFiles)
	if err != nil {
		return err
	}
	t.changes = append(t.changes, staleChanges...)
	manifestChange := &boardOutputFileChange{
		rel: boardOutputManifestName, source: manifestSource,
	}

	allChanges := append(append([]*boardOutputFileChange(nil), t.changes...), manifestChange)
	if err := t.prepareBackups(allChanges); err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.dir || entry.rel == "." {
			continue
		}
		if err := t.ensureDirectory(entry.rel, entry.mode.Perm()); err != nil {
			return err
		}
	}
	for _, change := range t.changes {
		if err := t.applyChange(change); err != nil {
			return err
		}
	}
	if err := t.removeStaleDirectories(previous, current); err != nil {
		return err
	}
	if err := t.applyChange(manifestChange); err != nil {
		return err
	}
	return nil
}

func (t *boardOutputTransaction) staleFileChanges(previous boardOutputManifest, currentFiles map[string]string) ([]*boardOutputFileChange, error) {
	var changes []*boardOutputFileChange
	for _, file := range previous.Files {
		rel := filepath.FromSlash(file.Path)
		if currentRel, ok := currentFiles[boardOutputCollisionKey(rel)]; ok {
			if currentRel == rel || sameBoardOutputPathRoot(t.root, rel, currentRel) {
				continue
			}
		}
		info, err := t.root.handle.Lstat(rel)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		digest, err := boardOutputRootFileDigest(t.root, rel, info)
		if err != nil {
			return nil, fmt.Errorf("hash prior board output %q: %w", rel, err)
		}
		if digest != strings.ToLower(file.SHA256) {
			// The user changed the file after D2 generated it. Preserve it and
			// relinquish ownership in the new manifest.
			continue
		}
		changes = append(changes, &boardOutputFileChange{
			rel: rel, remove: true,
			expectedSHA256: strings.ToLower(file.SHA256),
		})
	}
	return changes, nil
}

func (t *boardOutputTransaction) prepareBackups(changes []*boardOutputFileChange) error {
	var backupRoot string
	for index, change := range changes {
		info, err := t.root.handle.Lstat(change.rel)
		if errors.Is(err, os.ErrNotExist) {
			if change.remove {
				continue
			}
			change.existed = false
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("board output path %q is not a regular file", t.root.display(change.rel))
		}
		change.existed = true
		change.originalInfo = info
		change.originalMode = info.Mode().Perm()
		if backupRoot == "" {
			backupRoot, err = os.MkdirTemp(t.workspace.stageRoot, ".d2-board-rollback-")
			if err != nil {
				return fmt.Errorf("create board output rollback directory: %w", err)
			}
		}
		change.backup = filepath.Join(backupRoot, fmt.Sprintf("%06d", index))
		if err := copyBoardOutputRootFile(t.root, change.rel, change.backup, change.originalInfo, change.originalMode, true); err != nil {
			return fmt.Errorf("back up board output %q: %w", t.root.display(change.rel), err)
		}
		if change.remove {
			digest, err := boardOutputFileDigest(change.backup)
			if err != nil {
				return fmt.Errorf("verify board output backup %q: %w", t.root.display(change.rel), err)
			}
			if digest != change.expectedSHA256 {
				// The file was edited while publication was being prepared. Keep it
				// and drop it from the new manifest rather than deleting user data.
				change.skip = true
			}
		}
	}
	return nil
}

func (t *boardOutputTransaction) ensureDirectory(rel string, mode fs.FileMode) error {
	path := t.root.display(rel)
	info, err := t.root.handle.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		if err := t.beforeMutation(rel); err != nil {
			return err
		}
		if err := t.root.handle.Mkdir(rel, mode); err != nil {
			return fmt.Errorf("create board output directory %q: %w", path, err)
		}
		t.createdDirs = append(t.createdDirs, rel)
		t.visibleMutation = true
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("board output directory %q is not a real directory", path)
	}
	return nil
}

func (t *boardOutputTransaction) applyChange(change *boardOutputFileChange) error {
	if change.skip || change.remove && !change.existed {
		return nil
	}
	if err := t.beforeMutation(change.rel); err != nil {
		return err
	}
	if err := t.verifyBoardOutputFileState(change); err != nil {
		return err
	}
	if change.remove {
		digest, err := boardOutputRootFileDigest(t.root, change.rel, change.originalInfo)
		if err != nil {
			return fmt.Errorf("verify stale board output %q: %w", t.root.display(change.rel), err)
		}
		if digest != change.expectedSHA256 {
			// The user changed the file after it was backed up. Preserve it and
			// relinquish ownership in the manifest being published.
			change.skip = true
			return nil
		}
		if err := t.root.handle.Remove(change.rel); err != nil {
			return fmt.Errorf("remove stale board output %q: %w", t.root.display(change.rel), err)
		}
		change.applied = true
		t.appliedChanges = append(t.appliedChanges, change)
		t.visibleMutation = true
		return nil
	}

	applied, installedInfo, err := installBoardOutputFile(t.root, change.source, change.rel, change.existed)
	change.applied = applied
	change.installedInfo = installedInfo
	if applied {
		t.appliedChanges = append(t.appliedChanges, change)
		t.visibleMutation = true
	}
	if err != nil {
		return err
	}
	return nil
}

func (t *boardOutputTransaction) verifyBoardOutputFileState(change *boardOutputFileChange) error {
	path := t.root.display(change.rel)
	info, err := t.root.handle.Lstat(change.rel)
	if !change.existed {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("board output path %q appeared during publication", path)
	}
	if err != nil {
		return fmt.Errorf("inspect board output %q before publication: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(change.originalInfo, info) {
		return fmt.Errorf("board output path %q changed during publication", path)
	}
	return nil
}

func installBoardOutputFile(root *boardOutputRoot, source, destinationRel string, replacing bool) (applied bool, installedInfo fs.FileInfo, err error) {
	destination := root.display(destinationRel)
	temporaryRel, temporaryInfo, err := prepareBoardOutputFile(root, source, filepath.Dir(destinationRel))
	if err != nil {
		return false, nil, fmt.Errorf("prepare board output %q: %w", destination, err)
	}
	defer func() {
		if removeErr := removeBoardOutputFileIfSame(root, temporaryRel, temporaryInfo); removeErr != nil {
			err = errors.Join(err, removeErr)
		}
	}()

	if !replacing {
		if linkErr := root.handle.Link(temporaryRel, destinationRel); linkErr == nil {
			installedInfo, err := root.handle.Lstat(destinationRel)
			if err != nil {
				return true, nil, fmt.Errorf("inspect published board output %q: %w", destination, err)
			}
			return true, installedInfo, nil
		} else if _, statErr := root.handle.Lstat(destinationRel); statErr == nil {
			return false, nil, fmt.Errorf("publish board output %q: destination appeared during publication", destination)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return false, nil, fmt.Errorf("inspect board output %q after link failure: %w", destination, statErr)
		}

		applied, installedInfo, err := copyBoardOutputFileExclusive(root, temporaryRel, temporaryInfo, destinationRel)
		if err != nil {
			return applied, installedInfo, fmt.Errorf("publish board output %q: %w", destination, err)
		}
		return applied, installedInfo, nil
	}

	if err := root.handle.Rename(temporaryRel, destinationRel); err != nil {
		if removeErr := root.handle.Remove(destinationRel); removeErr != nil {
			return false, nil, fmt.Errorf("prepare board output %q for Windows replacement: %w", destination, removeErr)
		}
		applied = true
		if err := root.handle.Rename(temporaryRel, destinationRel); err != nil {
			return true, nil, fmt.Errorf("publish board output %q: %w", destination, err)
		}
	} else {
		applied = true
	}
	installedInfo, err = root.handle.Lstat(destinationRel)
	if err != nil {
		return true, nil, fmt.Errorf("inspect published board output %q: %w", destination, err)
	}
	return true, installedInfo, nil
}

func copyBoardOutputFileExclusive(root *boardOutputRoot, sourceRel string, sourceInfo fs.FileInfo, destinationRel string) (applied bool, installedInfo fs.FileInfo, err error) {
	input, err := openBoardOutputRootRegularFile(root, sourceRel, sourceInfo)
	if err != nil {
		return false, nil, err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	info, err := input.Stat()
	if err != nil {
		return false, nil, err
	}
	output, err := root.handle.OpenFile(destinationRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return false, nil, err
	}
	applied = true
	installedInfo, statErr := output.Stat()
	if statErr != nil {
		return true, nil, errors.Join(statErr, output.Close())
	}
	if _, err := io.Copy(output, input); err != nil {
		return true, installedInfo, errors.Join(err, output.Close())
	}
	if err := output.Chmod(info.Mode().Perm()); err != nil {
		return true, installedInfo, errors.Join(err, output.Close())
	}
	if err := output.Close(); err != nil {
		return true, installedInfo, err
	}
	return true, installedInfo, nil
}

func prepareBoardOutputFile(root *boardOutputRoot, source, destinationDirRel string) (temporaryRel string, temporaryInfo fs.FileInfo, err error) {
	input, err := os.Open(source)
	if err != nil {
		return "", nil, err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	info, err := input.Stat()
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("source is not a regular file")
	}
	temporaryRel, temporary, err := createBoardOutputTempFile(root, destinationDirRel)
	if err != nil {
		return "", nil, err
	}
	createdInfo, err := temporary.Stat()
	if err != nil {
		return "", nil, errors.Join(err, temporary.Close(), root.handle.Remove(temporaryRel))
	}
	remove := true
	closed := false
	defer func() {
		if !closed {
			closeErr := temporary.Close()
			if err == nil {
				err = closeErr
			} else {
				err = errors.Join(err, closeErr)
			}
		}
		if remove {
			if removeErr := removeBoardOutputFileIfSame(root, temporaryRel, createdInfo); removeErr != nil {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err := io.Copy(temporary, input); err != nil {
		return "", nil, err
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return "", nil, err
	}
	temporaryInfo, err = temporary.Stat()
	if err != nil {
		return "", nil, err
	}
	if err := temporary.Close(); err != nil {
		return "", nil, err
	}
	closed = true
	remove = false
	return temporaryRel, temporaryInfo, nil
}

func createBoardOutputTempFile(root *boardOutputRoot, directoryRel string) (string, *os.File, error) {
	for range 100 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, fmt.Errorf("generate temporary board output name: %w", err)
		}
		rel := filepath.Join(directoryRel, ".d2-board-file-"+hex.EncodeToString(random[:]))
		file, err := root.handle.OpenFile(rel, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return rel, file, nil
	}
	return "", nil, fmt.Errorf("create unique temporary board output file")
}

func removeBoardOutputFileIfSame(root *boardOutputRoot, rel string, expected fs.FileInfo) error {
	info, err := root.handle.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect temporary board output %q: %w", root.display(rel), err)
	}
	if !os.SameFile(expected, info) {
		return fmt.Errorf("refusing to remove changed temporary board output %q", root.display(rel))
	}
	if err := root.handle.Remove(rel); err != nil {
		return fmt.Errorf("remove temporary board output %q: %w", root.display(rel), err)
	}
	return nil
}

func (t *boardOutputTransaction) removeStaleDirectories(previous, current boardOutputManifest) error {
	currentDirs := make(map[string]string, len(current.Directories))
	for _, dir := range current.Directories {
		rel := filepath.FromSlash(dir)
		currentDirs[boardOutputCollisionKey(rel)] = rel
	}
	stale := make([]string, 0, len(previous.Directories))
	for _, dir := range previous.Directories {
		rel := filepath.FromSlash(dir)
		if currentRel, ok := currentDirs[boardOutputCollisionKey(rel)]; ok {
			if currentRel == rel || sameBoardOutputPathRoot(t.root, rel, currentRel) {
				continue
			}
		}
		stale = append(stale, rel)
	}
	sort.Slice(stale, func(i, j int) bool {
		return strings.Count(filepath.Clean(stale[i]), string(filepath.Separator)) > strings.Count(filepath.Clean(stale[j]), string(filepath.Separator))
	})
	for _, rel := range stale {
		info, err := t.root.handle.Lstat(rel)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		path := t.root.display(rel)
		children, err := readBoardOutputDirectory(t.root, rel, info)
		if err != nil {
			return err
		}
		if len(children) != 0 {
			continue
		}
		if err := t.beforeMutation(rel); err != nil {
			return err
		}
		currentInfo, statErr := t.root.handle.Lstat(rel)
		if statErr != nil || !os.SameFile(info, currentInfo) {
			return errors.Join(fmt.Errorf("board output directory %q changed during publication", path), statErr)
		}
		if err := t.root.handle.Remove(rel); err != nil {
			// A concurrent creator wins; preserve the directory rather than
			// recursively removing anything D2 does not own.
			if currentInfo, statErr := t.root.handle.Lstat(rel); statErr == nil {
				if children, readErr := readBoardOutputDirectory(t.root, rel, currentInfo); readErr == nil && len(children) != 0 {
					continue
				}
			}
			return fmt.Errorf("remove stale board output directory %q: %w", path, err)
		}
		t.removedDirs = append(t.removedDirs, boardOutputRemovedDirectory{rel: rel, mode: info.Mode().Perm()})
		t.visibleMutation = true
	}
	return nil
}

func sameBoardOutputPath(root, first, second string) bool {
	opened, err := openExistingBoardOutputRoot(root)
	if err != nil || opened == nil {
		return false
	}
	defer opened.close()
	return sameBoardOutputPathRoot(opened, first, second)
}

func sameBoardOutputPathRoot(root *boardOutputRoot, first, second string) bool {
	firstInfo, firstErr := root.handle.Lstat(first)
	secondInfo, secondErr := root.handle.Lstat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}

func readBoardOutputDirectory(root *boardOutputRoot, rel string, expected fs.FileInfo) (entries []fs.DirEntry, err error) {
	directory, err := root.handle.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	info, err := directory.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || !os.SameFile(expected, info) {
		return nil, fmt.Errorf("board output directory %q changed while opening it", root.display(rel))
	}
	return directory.ReadDir(-1)
}

func (t *boardOutputTransaction) beforeMutation(rel string) error {
	if t.workspace.beforePublish == nil {
		return nil
	}
	path := t.root.display(rel)
	if err := t.workspace.beforePublish(path); err != nil {
		return fmt.Errorf("publish board output %q: %w", path, err)
	}
	return nil
}

func (t *boardOutputTransaction) rollback() error {
	var rollbackErr error
	for index := len(t.removedDirs) - 1; index >= 0; index-- {
		dir := t.removedDirs[index]
		if err := t.root.handle.Mkdir(dir.rel, dir.mode); err != nil && !errors.Is(err, os.ErrExist) {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore board output directory %q: %w", t.root.display(dir.rel), err))
		}
	}
	for index := len(t.appliedChanges) - 1; index >= 0; index-- {
		change := t.appliedChanges[index]
		if err := t.rollbackBoardOutputFile(change); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	for index := len(t.createdDirs) - 1; index >= 0; index-- {
		rel := t.createdDirs[index]
		path := t.root.display(rel)
		if err := t.root.handle.Remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
			if info, statErr := t.root.handle.Lstat(rel); statErr == nil {
				if children, readErr := readBoardOutputDirectory(t.root, rel, info); readErr == nil && len(children) != 0 {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("refusing to remove non-empty transaction directory %q", path))
					continue
				}
			}
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove transaction directory %q: %w", path, err))
		}
	}
	if rollbackErr == nil {
		t.visibleMutation = false
	}
	return rollbackErr
}

func (t *boardOutputTransaction) rollbackBoardOutputFile(change *boardOutputFileChange) error {
	if !change.applied {
		return nil
	}
	path := t.root.display(change.rel)
	info, err := t.root.handle.Lstat(change.rel)
	if change.installedInfo != nil {
		if err != nil || !os.SameFile(change.installedInfo, info) {
			return fmt.Errorf("refusing to roll back changed board output %q", path)
		}
	} else if err == nil {
		return fmt.Errorf("refusing to roll back recreated board output %q", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if change.existed {
		_, _, err := installBoardOutputFile(t.root, change.backup, change.rel, change.installedInfo != nil)
		if err != nil {
			return fmt.Errorf("restore board output %q: %w", path, err)
		}
		return nil
	}
	if change.installedInfo == nil {
		return nil
	}
	if err := t.root.handle.Remove(change.rel); err != nil {
		return fmt.Errorf("remove newly published board output %q: %w", path, err)
	}
	return nil
}
