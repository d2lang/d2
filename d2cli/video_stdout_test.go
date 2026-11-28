package d2cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Use an inherited, unread OS pipe, just like "d2 ... - | slow-consumer".
// A Go-created pipe can be pollable even when one inherited from a shell isn't.
func TestMP4BlockedStdout(t *testing.T) {
	const helper = "D2_TEST_BLOCKED_VIDEO_STDOUT"
	if os.Getenv(helper) == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := copyVideoStdout(ctx, os.Stdout, bytes.NewReader(make([]byte, 8<<20)))
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			os.Exit(1)
		}
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMP4BlockedStdout$")
	cmd.Env = append(os.Environ(), helper+"=1")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	cmd.Stdout = writer
	if err := cmd.Run(); err != nil {
		t.Fatalf("stdout cancellation failed (deadline %v): %v", ctx.Err(), err)
	}
}
