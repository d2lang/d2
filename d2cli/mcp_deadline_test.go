//go:build !d2nomcp

package d2cli

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCPHandlerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &mcpServer{cfg: mcpConfig{timeout: time.Second}, sem: make(chan struct{}, 1)}
		rendered := &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: []byte("rendered"), MIMEType: "image/png"}}}
		info := renderInfo{mcpStatus: mcpStatus{OK: true}, Format: "png"}

		// An encoder can finish the image between its last context check and
		// the wrapper receiving the result.
		render := mcpHandler(s, "render", func(ctx context.Context, _ *mcp.CallToolRequest, _ renderInput) (*mcp.CallToolResult, any, error) {
			<-ctx.Done()
			return rendered, info, nil
		})
		result, out, err := render(t.Context(), nil, renderInput{})
		require.NoError(t, err)
		require.Same(t, rendered, result)
		require.Equal(t, info, out)

		check := mcpHandler(s, "check", func(ctx context.Context, _ *mcp.CallToolRequest, _ checkInput) (*mcp.CallToolResult, any, error) {
			<-ctx.Done()
			return mcpFailure(ctx.Err().Error())
		})
		result, _, err = check(t.Context(), nil, checkInput{})
		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Equal(t, "timed out after 1s", result.Content[0].(*mcp.TextContent).Text)
	})
}

func TestMCPQueuedHandlerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &mcpServer{cfg: mcpConfig{timeout: time.Minute}, sem: make(chan struct{}, 1)}
		s.sem <- struct{}{}
		render := mcpHandler(s, "render", func(context.Context, *mcp.CallToolRequest, renderInput) (*mcp.CallToolResult, any, error) {
			t.Error("dispatched a canceled request")
			return mcpFailure("unexpected dispatch")
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan *mcp.CallToolResult, 1)
		go func() {
			r, _, _ := render(ctx, nil, renderInput{})
			done <- r
		}()
		synctest.Wait()
		cancel()
		result := <-done
		require.True(t, result.IsError)
		require.Equal(t, "context canceled", result.Content[0].(*mcp.TextContent).Text)
	})
}
