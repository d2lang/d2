package lspserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/assert"
)

func TestFraming(t *testing.T) {
	t.Run("L1_RoundTrip", func(t *testing.T) {
		var buf bytes.Buffer
		c := conn{r: bufio.NewReader(&buf), w: &buf}
		body := json.RawMessage(`{"message":"日本🌱"}`)
		assert.Success(t, c.write(body))
		assert.Equal(t, fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body), buf.String())
		got, err := c.read()
		assert.Success(t, err)
		assert.Equal(t, string(body), string(got))
	})
	t.Run("L2_HeaderVariants", func(t *testing.T) {
		c := conn{r: bufio.NewReader(strings.NewReader("Content-Type: application/json\ncontent-length: 2\n\n{}"))}
		got, err := c.read()
		assert.Success(t, err)
		assert.Equal(t, "{}", string(got))
	})
	t.Run("L3_InvalidLength", func(t *testing.T) {
		for _, input := range []string{"Content-Type: application/json\n\n", "Content-Length: -1\n\n", "Content-Length: 16777217\n\n"} {
			c := conn{r: bufio.NewReader(strings.NewReader(input))}
			_, err := c.read()
			assert.Error(t, err)
		}
	})
}

func TestLifecycle(t *testing.T) {
	t.Run("L4_Initialize", func(t *testing.T) {
		c := newBareTestClient(t)
		type result struct {
			Capabilities struct {
				PositionEncoding string
				TextDocumentSync struct {
					Change int
				}
			}
			ServerInfo struct {
				Name, Version string
			}
		}
		got := decode[result](t, c.request("initialize", map[string]any{}))
		assert.Equal(t, "utf-16", got.Capabilities.PositionEncoding)
		assert.Equal(t, 1, got.Capabilities.TextDocumentSync.Change)
		assert.Equal(t, "d2", got.ServerInfo.Name)
		assert.Equal(t, "test-version", got.ServerInfo.Version)
	})
	t.Run("L5_BeforeInitialize", func(t *testing.T) {
		c := newBareTestClient(t)
		assert.Equal(t, -32002, c.requestError("shutdown", nil).Code)
	})
	t.Run("L6_SecondInitialize", func(t *testing.T) {
		c := newTestClient(t)
		assert.Equal(t, -32600, c.requestError("initialize", map[string]any{}).Code)
	})
	t.Run("L7_UnknownMethods", func(t *testing.T) {
		c := newTestClient(t)
		c.notify("unknown/notification", nil)
		c.notify("$/cancelRequest", map[string]int{"id": 19})
		assert.Equal(t, -32601, c.requestError("foo/bar", nil).Code)
		assert.Equal(t, 0, len(c.pending))
	})
	t.Run("L8_InvalidJSON", func(t *testing.T) {
		c := newBareTestClient(t)
		c.sendRaw("{not json")
		m := c.receive(time.After(responseTimeout))
		assert.Equal(t, "null", string(m.ID))
		assert.True(t, m.Result == nil && m.Error != nil)
		assert.Equal(t, -32700, m.Error.Code)
		c.request("initialize", map[string]any{})
	})
	t.Run("L9_ShutdownAndExit", func(t *testing.T) {
		c := newTestClient(t)
		assert.Equal(t, "null", string(c.request("shutdown", nil)))
		assert.Equal(t, -32600, c.requestError("foo/bar", nil).Code)
		c.notify("exit", nil)
		assert.Equal(t, 0, c.waitExit())
	})
	t.Run("L10_ExitWithoutShutdown", func(t *testing.T) {
		c := newBareTestClient(t)
		c.notify("exit", nil)
		assert.Equal(t, 1, c.waitExit())
	})
	t.Run("L10_EOF", func(t *testing.T) {
		c := newTestClient(t)
		c.in.Close()
		assert.Equal(t, 1, c.waitExit())
	})
}

func TestL11_FileURIs(t *testing.T) {
	p, ok := uriToPathOS("file:///a%20b/c.d2", "linux")
	assert.True(t, ok)
	assert.Equal(t, "/a b/c.d2", p)
	localPath := filepath.Join(t.TempDir(), "日本 map.d2")
	p, ok = uriToPath(pathToURI(localPath))
	assert.True(t, ok)
	assert.Equal(t, localPath, p)
	p, ok = uriToPathOS("file:///c%3A/x/y.d2", "windows")
	assert.True(t, ok)
	assert.Equal(t, `c:\x\y.d2`, p)
	_, ok = uriToPath("untitled:Untitled-1")
	assert.False(t, ok)
}

func TestWatchedFilesRegistration(t *testing.T) {
	c := newBareTestClient(t)
	c.request("initialize", json.RawMessage(`{"capabilities":{"workspace":{"didChangeWatchedFiles":{"dynamicRegistration":true}}}}`))
	c.notify("initialized", nil)
	m := c.receive(time.After(responseTimeout))
	assert.Equal(t, "client/registerCapability", m.Method)
	assert.True(t, m.ID != nil)
	var params struct {
		Registrations []struct {
			Method          string
			RegisterOptions struct {
				Watchers []struct{ GlobPattern string }
			}
		}
	}
	assert.Success(t, json.Unmarshal(m.Params, &params))
	assert.Equal(t, 1, len(params.Registrations))
	registration := params.Registrations[0]
	assert.Equal(t, "workspace/didChangeWatchedFiles", registration.Method)
	assert.Equal(t, 1, len(registration.RegisterOptions.Watchers))
	assert.Equal(t, "**/*.d2", registration.RegisterOptions.Watchers[0].GlobPattern)
	assert.Success(t, c.conn.write(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil}))
	c.request("shutdown", nil)
	assert.Equal(t, 0, len(c.pending))
}
