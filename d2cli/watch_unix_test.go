//go:build unix

package d2cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/d2lang/d2/lib/localfile"
)

func TestTrackedFSRejectsFIFOWithoutBlocking(t *testing.T) {
	fifoPath := filepath.Join(t.TempDir(), "import.d2")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}
	tracked := trackedFS{localFiles: localfile.Unrestricted()}
	done := make(chan error, 1)
	go func() {
		file, err := tracked.Open(fifoPath)
		if file != nil {
			err = file.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("trackedFS accepted a FIFO")
		}
		if len(tracked.opened) != 0 {
			t.Fatalf("FIFO was tracked as opened: %q", tracked.opened)
		}
	case <-time.After(2 * time.Second):
		writer, _ := os.OpenFile(fifoPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if writer != nil {
			_ = writer.Close()
		}
		t.Fatal("trackedFS blocked on a FIFO")
	}
}
