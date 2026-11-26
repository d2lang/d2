//go:build !d2nomcp

package d2cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/d2lang/util-go/xmain"
)

// A pipeline closes stdin before the server starts handling the requests.
func TestMCPTransportHalfClose(t *testing.T) {
	var out bytes.Buffer
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n")
	s := mcp.NewServer(&mcp.Implementation{Name: "d2", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "guide", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	require.NoError(t, s.Run(ctx, mcpTransport(&xmain.State{Stdin: strings.NewReader(input), Stdout: mcpWriteCloser{&out}})))
	var responses []int
	for line := range strings.Lines(out.String()) {
		var reply struct {
			ID     int
			Result json.RawMessage
		}
		require.NoError(t, json.Unmarshal([]byte(line), &reply))
		require.NotEmpty(t, reply.Result)
		responses = append(responses, reply.ID)
	}
	require.Equal(t, []int{1, 2}, responses)
}

func TestMCPTransportShutdown(t *testing.T) {
	const ping = `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	const listen = `{"jsonrpc":"2.0","id":2,"method":"subscriptions/listen","params":{"notifications":{}}}`
	for _, tc := range []struct{ name, input string }{
		{"open subscription", listen},
		{"legacy batch with notification", `[` + ping + `,{"jsonrpc":"2.0","method":"notifications/initialized"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			conn, err := mcpTransport(&xmain.State{Stdin: strings.NewReader(tc.input), Stdout: mcpWriteCloser{io.Discard}}).Connect(ctx)
			require.NoError(t, err)
			defer conn.Close()
			for {
				if _, err = conn.Read(ctx); err != nil {
					break
				}
			}
			require.ErrorIs(t, err, io.EOF)
			require.NoError(t, ctx.Err())
		})
	}

	t.Run("closed output", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		stdin, clientInput := io.Pipe()
		defer clientInput.Close()
		stdout, serverOutput := io.Pipe()
		require.NoError(t, stdout.Close())
		conn, err := mcpTransport(&xmain.State{Stdin: stdin, Stdout: serverOutput}).Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()
		_, err = io.WriteString(clientInput, ping+"\n")
		require.NoError(t, err)
		msg, err := conn.Read(ctx)
		require.NoError(t, err)
		err = conn.Write(ctx, &jsonrpc.Response{ID: msg.(*jsonrpc.Request).ID, Result: json.RawMessage("{}")})
		require.ErrorIs(t, err, io.ErrClosedPipe)
		_, err = conn.Read(ctx)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("canceled with blocked output", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		stdout, serverOutput := io.Pipe()
		defer stdout.Close()
		out := startingWrite{serverOutput, make(chan struct{})}
		conn, err := mcpTransport(&xmain.State{Stdin: strings.NewReader(ping), Stdout: out}).Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()
		msg, err := conn.Read(ctx)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() {
			done <- conn.Write(ctx, &jsonrpc.Response{ID: msg.(*jsonrpc.Request).ID, Result: json.RawMessage("{}")})
		}()
		<-out.started
		cancel()
		select {
		case err := <-done:
			require.ErrorIs(t, err, io.ErrClosedPipe)
		case <-time.After(3 * time.Second):
			t.Fatal("cancellation did not unblock stdout")
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		for _, input := range []string{"{", strings.Repeat(" ", mcp.DefaultMaxLineLength+1)} {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			conn, err := mcpTransport(&xmain.State{Stdin: strings.NewReader(input), Stdout: mcpWriteCloser{io.Discard}}).Connect(ctx)
			require.NoError(t, err)
			_, err = conn.Read(ctx)
			require.Error(t, err)
			require.False(t, errors.Is(err, context.DeadlineExceeded))
			require.NoError(t, conn.Close())
			cancel()
		}
	})
}

type startingWrite struct {
	io.WriteCloser
	started chan struct{}
}

func (w startingWrite) Write(p []byte) (int, error) {
	close(w.started)
	return w.WriteCloser.Write(p)
}

func TestMCPErrorContentPreservesProtocolErrors(t *testing.T) {
	session := newTestMCP(t, mcpConfig{})
	_, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "unavailable"})
	var protocolErr *jsonrpc.Error
	require.ErrorAs(t, err, &protocolErr)
	require.EqualValues(t, jsonrpc.CodeInvalidParams, protocolErr.Code)
	require.Contains(t, protocolErr.Message, "unavailable")
	require.False(t, callMCP(t, session, "check", map[string]any{"source": "a -> b"}).IsError)
}
