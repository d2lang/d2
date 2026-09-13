// Package localfile provides explicit policies for opening regular local files.
// The zero value denies access. Callers must deliberately choose either a
// symlink-safe filesystem root or unrestricted host-filesystem access.
//
// On Unix, files are opened nonblocking and verified from the opened descriptor
// before Open returns, preventing ordinary FIFO open waits. Device-specific
// open behavior can still vary. Other platforms have no portable nonblocking
// open flag, so a hostile special file may block before it can be rejected. In
// addition, os.Root cannot provide a symlink security boundary on GOOS=js and
// does not retain directory identity across renames on GOOS=js or GOOS=plan9.
// Callers on those platforms must not treat Rooted as a complete boundary for
// hostile filesystems.
package localfile

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// ErrDenied reports that a policy does not permit a local-file path.
var ErrDenied = errors.New("localfile: access denied")

type policyMode uint8

const (
	modeDenied policyMode = iota
	modeRooted
	modeUnrestricted
)

// Policy controls access to local files. Its zero value denies all local-file
// access and is safe for untrusted input.
//
// Policy implements the part of fs.FS needed by D2's compiler imports. Open
// intentionally rejects directories and other non-regular files, so Policy is
// not a general-purpose directory-browsing filesystem.
type Policy struct {
	mode       policyMode
	rootPath   string
	root       *rootState
	cacheScope [32]byte
}

// rootState makes Policy copies share exactly one close operation. Policy is a
// value type and is commonly copied into compiler and asset resolver options.
type rootState struct {
	handle *os.Root
	once   sync.Once
	err    error
}

// Rooted returns a policy that permits files at or beneath root. Opens use
// one retained os.Root, so symbolic links cannot escape root and replacing the
// configured path cannot retarget the policy on supported platforms.
func Rooted(root string) (Policy, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Policy{}, fmt.Errorf("localfile: resolve root %q: %w", root, err)
	}
	absolute = filepath.Clean(absolute)
	handle, err := os.OpenRoot(absolute)
	if err != nil {
		return Policy{}, fmt.Errorf("localfile: open root %q: %w", root, err)
	}
	var cacheScope [32]byte
	if _, err := rand.Read(cacheScope[:]); err != nil {
		return Policy{}, errors.Join(
			fmt.Errorf("localfile: create cache scope for root %q: %w", root, err),
			closeRoot(handle, root),
		)
	}
	return Policy{mode: modeRooted, rootPath: absolute, root: &rootState{handle: handle}, cacheScope: cacheScope}, nil
}

// Unrestricted returns a policy that permits arbitrary host-filesystem reads.
// Use it only for trusted local input, such as the D2 command-line interface.
func Unrestricted() Policy {
	return Policy{mode: modeUnrestricted}
}

// RootPath returns the absolute configured root and true for a rooted policy.
// It returns an empty string and false for denied and unrestricted policies.
func (p Policy) RootPath() (string, bool) {
	return p.rootPath, p.mode == modeRooted
}

// Close releases the retained OS root handle. Copies of a rooted Policy share
// one copy-safe close operation and return the same close result. It does not
// erase or revoke immutable resources that callers already retained in their
// own caches. Closing a denied or unrestricted policy is a no-op.
func (p Policy) Close() error {
	if p.mode != modeRooted || p.root == nil {
		return nil
	}
	p.root.once.Do(func() {
		p.root.err = closeRoot(p.root.handle, p.rootPath)
	})
	return p.root.err
}

// CacheKey returns a policy-scoped, canonical key for name. It rejects names
// outside the policy's lexical scope; Open additionally enforces the rooted
// symbolic-link boundary. The key prevents a cache filled under an unrestricted
// policy from bypassing a rooted or denied policy.
func (p Policy) CacheKey(name string) (string, error) {
	resolved, err := p.resolve(name)
	if err != nil {
		return "", err
	}
	switch p.mode {
	case modeRooted:
		// The random per-policy scope prevents a cache entry produced through an
		// older directory at the same path from crossing into a replacement root.
		return fmt.Sprintf("root:%x:%s", p.cacheScope, filepath.ToSlash(resolved.relative)), nil
	case modeUnrestricted:
		return "unrestricted:" + filepath.ToSlash(resolved.absolute), nil
	default:
		panic("localfile: resolved path under invalid policy mode")
	}
}

// Open opens a regular file according to the policy. For rooted policies,
// relative names are interpreted relative to the configured root. Absolute
// names are accepted only when they are lexically beneath that root, and the
// retained os.Root enforces the same boundary while following symbolic links.
func (p Policy) Open(name string) (fs.File, error) {
	resolved, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	if p.mode == modeUnrestricted {
		file, err := openFile(resolved.absolute)
		if err != nil {
			return nil, err
		}
		return requireRegular(name, file)
	}

	if p.root == nil || p.root.handle == nil {
		return nil, errors.New("localfile: invalid rooted policy")
	}
	file, err := openFileInRoot(p.root.handle, resolved.relative)
	if err != nil {
		return nil, fmt.Errorf("localfile: open %q beneath root %q: %w", name, p.rootPath, err)
	}
	return requireRegular(name, file)
}

func requireRegular(name string, file *os.File) (fs.File, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("localfile: stat opened file %q: %w", name, err),
			closeFile(file, name),
		)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.Join(
			fmt.Errorf("localfile: %q is not a regular file", name),
			closeFile(file, name),
		)
	}
	return file, nil
}

func closeFile(file *os.File, name string) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("localfile: close %q: %w", name, err)
	}
	return nil
}

func closeRoot(root *os.Root, name string) error {
	if err := root.Close(); err != nil {
		return fmt.Errorf("localfile: close root %q: %w", name, err)
	}
	return nil
}

type resolvedPath struct {
	absolute string
	relative string
}

func (p Policy) resolve(name string) (resolvedPath, error) {
	switch p.mode {
	case modeDenied:
		return resolvedPath{}, fmt.Errorf("%w: local-file access is disabled", ErrDenied)
	case modeUnrestricted:
		absolute, err := filepath.Abs(name)
		if err != nil {
			return resolvedPath{}, fmt.Errorf("localfile: resolve %q: %w", name, err)
		}
		return resolvedPath{absolute: filepath.Clean(absolute)}, nil
	case modeRooted:
		var relative string
		if filepath.IsAbs(name) {
			var err error
			relative, err = filepath.Rel(p.rootPath, filepath.Clean(name))
			if err != nil {
				return resolvedPath{}, fmt.Errorf("localfile: resolve %q beneath root %q: %w", name, p.rootPath, err)
			}
		} else {
			relative = filepath.Clean(name)
		}
		if !filepath.IsLocal(relative) {
			return resolvedPath{}, fmt.Errorf("%w: %q is outside configured root %q", ErrDenied, name, p.rootPath)
		}
		return resolvedPath{
			absolute: filepath.Join(p.rootPath, relative),
			relative: relative,
		}, nil
	default:
		return resolvedPath{}, errors.New("localfile: invalid policy")
	}
}
