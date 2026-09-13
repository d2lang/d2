//go:build unix

package localfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestPolicyRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	rooted, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rooted.Close() })
	for _, tc := range []struct {
		name   string
		policy Policy
		path   string
	}{
		{name: "rooted", policy: rooted, path: "fifo-rooted"},
		{name: "unrestricted", policy: Unrestricted(), path: filepath.Join(root, "fifo-unrestricted")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fifoPath := tc.path
			if !filepath.IsAbs(fifoPath) {
				fifoPath = filepath.Join(root, fifoPath)
			}
			if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
				t.Fatal(err)
			}

			done := make(chan error, 1)
			go func() {
				file, err := tc.policy.Open(tc.path)
				if file != nil {
					err = file.Close()
				}
				done <- err
			}()

			select {
			case err := <-done:
				if err == nil {
					t.Fatal("Open accepted a FIFO")
				}
			case <-time.After(2 * time.Second):
				// Release a blocking reader so a regression does not strand a
				// goroutine for the remainder of the test process.
				writer, _ := os.OpenFile(fifoPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
				if writer != nil {
					_ = writer.Close()
				}
				t.Fatal("Open blocked on a FIFO")
			}
		})
	}
}
