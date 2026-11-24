package d2cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/util-go/xmain"
)

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

func TestRunWithPanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("recovers panic and prints issue link", func(t *testing.T) {
		t.Parallel()

		var stderr bytes.Buffer
		ms := &xmain.State{Stderr: nopWriteCloser{&stderr}}

		err := runWithPanicRecovery(context.Background(), ms, func(context.Context, *xmain.State) error {
			panic("unexpected nil node")
		})

		var exitErr xmain.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected xmain.ExitError, got %T (%v)", err, err)
		}
		if exitErr.Code != 2 {
			t.Fatalf("exit code = %d, want 2", exitErr.Code)
		}
		if exitErr.Message != "" {
			t.Fatalf("exit message = %q, want empty", exitErr.Message)
		}

		out := stderr.String()
		if !strings.Contains(out, "d2 panicked: unexpected nil node") {
			t.Fatalf("stderr missing panic message:\n%s", out)
		}
		if !strings.Contains(out, issueReportURL) {
			t.Fatalf("stderr missing issue link %q:\n%s", issueReportURL, out)
		}
		if !strings.Contains(out, "TestRunWithPanicRecovery") {
			t.Fatalf("stderr missing stack trace:\n%s", out)
		}
	})

	t.Run("passes through normal errors unchanged", func(t *testing.T) {
		t.Parallel()

		want := errors.New("compile failed")
		var stderr bytes.Buffer
		ms := &xmain.State{Stderr: nopWriteCloser{&stderr}}

		err := runWithPanicRecovery(context.Background(), ms, func(context.Context, *xmain.State) error {
			return want
		})

		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
		if stderr.Len() != 0 {
			t.Fatalf("expected empty stderr, got %q", stderr.String())
		}
	})
}
