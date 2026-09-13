package d2cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"
	"github.com/d2lang/util-go/xos"
)

func TestHelpWithPrivateNetworkEnvironment(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout bytes.Buffer
	state := &xmain.TestState{
		Run:    Run,
		Env:    xos.NewEnv([]string{"D2_ALLOW_PRIVATE_NETWORK=1"}),
		Args:   []string{"d2", "--help"},
		Stdout: &stdout,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	if err := state.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if output := stdout.String(); !strings.Contains(output, "--allow-private-network") || !strings.Contains(output, "$D2_ALLOW_PRIVATE_NETWORK") {
		t.Fatalf("help does not document private-network opt-in:\n%s", output)
	}
}

func TestPrivateNetworkEnvDefaultRejectsInvalidValue(t *testing.T) {
	_, err := privateNetworkEnvDefault(&xmain.State{Env: xos.NewEnv([]string{"D2_ALLOW_PRIVATE_NETWORK=yes"})})
	var usageErr xmain.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("privateNetworkEnvDefault() error = %v, want xmain.UsageError", err)
	}
}

func TestLayoutCmdRejectsExtraArguments(t *testing.T) {
	opts := xmain.NewOpts(xos.NewEnv(nil), []string{"layout", "dagre", "info"})
	if err := opts.Flags.Parse(opts.Args); err != nil {
		t.Fatal(err)
	}

	err := layoutCmd(context.Background(), &xmain.State{Opts: opts})
	var usageErr xmain.UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("layoutCmd() error = %v, want xmain.UsageError", err)
	}
	const want = "bad usage: layout subcommand accepts at most one argument"
	if err.Error() != want {
		t.Fatalf("layoutCmd() error = %q, want %q", err, want)
	}
}
