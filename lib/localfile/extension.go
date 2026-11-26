//go:build !tinygo || !js || !wasm

package localfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FilesWithExtension returns a view of a rooted policy for reading source
// files. Both the requested name and the symlink target must have ext (for
// example ".d2"). The view shares the policy's root and import path semantics.
func (p Policy) FilesWithExtension(ext string) fs.FS {
	return extensionFS{Policy: p, ext: ext}
}

type extensionFS struct {
	Policy
	ext string
}

func (v extensionFS) Open(name string) (fs.File, error) {
	path, err := v.resolve(name)
	if err != nil {
		return nil, err
	}
	if v.root == nil {
		return nil, ErrDenied
	}
	wrongExtension := func() error {
		return fmt.Errorf("%w: expected a %s file", ErrDenied, v.ext)
	}
	if !strings.EqualFold(filepath.Ext(path.relative), v.ext) {
		return nil, wrongExtension()
	}
	root := v.root.handle
	relative := path.relative
	const maxSymlinks = 40
	for links := 0; links <= maxSymlinks; links++ {
		info, err := root.Lstat(relative)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := root.Readlink(relative)
			if err != nil {
				return nil, err
			}
			if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
				return nil, ErrDenied
			}
			// Preserve "..": an earlier directory component may itself be
			// a symlink. os.Root resolves it and enforces containment.
			dir, _ := filepath.Split(relative)
			relative = dir + target
			continue
		}
		if !strings.EqualFold(filepath.Ext(relative), v.ext) {
			return nil, wrongExtension()
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("localfile: %q is not a regular file", name)
		}
		file, err := openFileInRoot(root, relative)
		if err != nil {
			return nil, err
		}
		// Check the opened descriptor before returning it. A replacement
		// between Lstat and Open must not change which file is read.
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return nil, errors.Join(ErrDenied, err, file.Close())
		}
		return file, nil
	}
	return nil, fmt.Errorf("%w: too many symbolic links in %q", ErrDenied, name)
}
