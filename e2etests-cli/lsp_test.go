package e2etests_cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/lib/version"
)

// Exercise both CLI spellings and consume every byte written to stdout (L12, L77).
func TestLSPStdio(t *testing.T) {
	for _, args := range [][]string{{"lsp"}, {"lsp", "--stdio"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			ts := testMain(t.TempDir(), nil, args...)
			in := ts.StdinPipe()
			out := bufio.NewReader(ts.StdoutPipe())
			ts.Start(t, ctx)
			defer ts.Cleanup(t)

			send := func(body string) {
				t.Helper()
				if _, err := fmt.Fprintf(in, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
					t.Fatal(err)
				}
			}
			send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`)
			var init struct {
				ServerInfo struct {
					Name    string
					Version string
				}
			}
			if err := json.Unmarshal(readLSPResult(t, out, 1), &init); err != nil || init.ServerInfo.Name != "d2" || init.ServerInfo.Version != version.Version {
				t.Fatalf("unexpected initialize result: %+v, %v", init, err)
			}
			send(`{"jsonrpc":"2.0","method":"initialized","params":{}}`)
			send(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
			if got := string(readLSPResult(t, out, 2)); got != "null" {
				t.Fatalf("shutdown result = %s", got)
			}
			send(`{"jsonrpc":"2.0","method":"exit"}`)
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if trailing, err := io.ReadAll(out); len(trailing) != 0 || err != nil {
				t.Fatalf("unexpected trailing stdout: %q, %v", trailing, err)
			}
			if err := ts.Wait(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLSPInterruptWithOpenStdin(t *testing.T) {
	ts := testMain(t.TempDir(), nil, "lsp")
	ts.StdinPipe()
	ts.Start(t, t.Context())
	defer ts.Cleanup(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := ts.Signal(ctx, os.Interrupt); err != nil {
		t.Fatal(err)
	}
	var exit xmain.ExitError
	if err := ts.Wait(ctx); !errors.As(err, &exit) || exit.Code != 1 {
		t.Fatalf("expected prompt interrupt exit, got %v", err)
	}
}

func readLSPResult(t *testing.T, out *bufio.Reader, id int) json.RawMessage {
	t.Helper()
	header, err := out.ReadString('\n')
	length, errLength := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(header, "Content-Length: "), "\r\n"))
	if err != nil || errLength != nil || length < 0 || length > 16<<20 || header != fmt.Sprintf("Content-Length: %d\r\n", length) {
		t.Fatalf("invalid frame header: %q, %v", header, err)
	}
	if blank, err := out.ReadString('\n'); blank != "\r\n" || err != nil {
		t.Fatalf("invalid header terminator: %q, %v", blank, err)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(out, body); err != nil {
		t.Fatal(err)
	}
	var response struct {
		JSONRPC string
		ID      int
		Result  json.RawMessage
		Error   json.RawMessage
	}
	if err := json.Unmarshal(body, &response); err != nil || response.JSONRPC != "2.0" || response.ID != id || response.Result == nil || response.Error != nil {
		t.Fatalf("unexpected response: %s, %v", body, err)
	}
	return response.Result
}

func TestLSPFlags(t *testing.T) {
	for _, flag := range []string{"--help", "-h", "extra", "--unknown"} {
		t.Run(flag, func(t *testing.T) {
			ts := testMain(t.TempDir(), nil, "lsp", flag)
			var out bytes.Buffer
			ts.Stdout = &out
			ts.Start(t, t.Context())
			defer ts.Cleanup(t)
			err := ts.Wait(t.Context())
			if flag == "--help" || flag == "-h" {
				if err != nil || !strings.Contains(out.String(), "d2 lsp [--stdio]") {
					t.Fatalf("help: stdout %q, %v", out.String(), err)
				}
				return
			}
			var usage xmain.UsageError
			if !errors.As(err, &usage) || out.Len() != 0 || (flag == "extra" && usage.Message != "lsp takes no arguments") {
				t.Fatalf("expected usage error for %q: stdout %q, %v", flag, out.String(), err)
			}
		})
	}
}
