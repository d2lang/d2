//go:build unix

package d2cli

import (
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// A pipe inherited from the shell can have a blocking descriptor outside Go's
// poller. Give the transfer its own pollable file so Close can wake Write.
// Dup shares file status flags; restore the caller's blocking mode afterwards.
func videoPipeWriter(dst io.WriteCloser) (io.WriteCloser, func() error, error) {
	file, ok := dst.(*os.File)
	if !ok {
		return dst, nil, nil
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if stat.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 || file.SetWriteDeadline(time.Time{}) == nil {
		return dst, nil, nil
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var fd, flags int
	var setupErr error
	err = raw.Control(func(original uintptr) {
		flags, setupErr = unix.FcntlInt(original, unix.F_GETFL, 0)
		if setupErr != nil {
			return
		}
		syscall.ForkLock.RLock()
		fd, setupErr = unix.Dup(int(original))
		if setupErr == nil {
			unix.CloseOnExec(fd)
		}
		syscall.ForkLock.RUnlock()
		if setupErr == nil {
			setupErr = unix.SetNonblock(fd, true)
			if setupErr != nil {
				unix.Close(fd)
			}
		}
	})
	if err != nil || setupErr != nil {
		return nil, nil, errors.Join(err, setupErr)
	}
	pollable := os.NewFile(uintptr(fd), file.Name())
	release := func() error {
		var restoreErr error
		if flags&unix.O_NONBLOCK == 0 {
			err := raw.Control(func(original uintptr) { restoreErr = unix.SetNonblock(int(original), false) })
			restoreErr = errors.Join(err, restoreErr)
		}
		return errors.Join(restoreErr, file.Close())
	}
	return pollable, release, nil
}
