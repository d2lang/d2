package d2cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"
)

func TestFmtHonorsCanceledContextBeforeWriting(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.d2")
	input := []byte("x:{y}\n")
	if err := os.WriteFile(inputPath, input, 0o600); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &xmain.TestState{
		Run:  Run,
		Args: []string{"d2", "fmt", inputPath},
		PWD:  directory,
	}
	state.Start(t, runCtx)
	defer state.Cleanup(t)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer waitCancel()
	if err := state.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("d2 fmt error = %v, want context.Canceled", err)
	}

	got, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(input) {
		t.Fatalf("d2 fmt rewrote input after cancellation: got %q, want %q", got, input)
	}
}
