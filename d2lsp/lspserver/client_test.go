package lspserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/d2lang/util-go/assert"
)

const responseTimeout = 5 * time.Second

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// Read stdout continuously. A server can publish diagnostics while the test
// client is writing its next request through an unbuffered pipe.
type testClient struct {
	t       *testing.T
	in      *io.PipeWriter
	conn    *conn
	id      int
	inbox   chan []byte
	pending []wireMessage
	done    chan struct{}
	code    int
}

func newBareTestClient(t *testing.T) *testClient {
	t.Helper()
	serverIn, in := io.Pipe()
	out, serverOut := io.Pipe()
	c := &testClient{t: t, in: in, conn: &conn{w: in}, inbox: make(chan []byte, 128), done: make(chan struct{})}
	stopReading := make(chan struct{})
	go func() {
		defer close(c.inbox)
		frames := conn{r: bufio.NewReader(out)}
		for {
			body, err := frames.read()
			if err != nil {
				return
			}
			select {
			case c.inbox <- body:
			case <-stopReading:
				return
			}
		}
	}()
	go func() {
		defer close(c.done)
		c.code = New(Options{In: serverIn, Out: serverOut, Version: "test-version"}).Run(context.Background())
		serverOut.Close()
		serverIn.Close()
	}()
	t.Cleanup(func() {
		close(stopReading)
		in.Close()
		out.Close()
		c.waitExit()
	})
	return c
}

func newTestClient(t *testing.T) *testClient {
	t.Helper()
	c := newBareTestClient(t)
	c.request("initialize", map[string]any{})
	c.notify("initialized", map[string]any{})
	return c
}

func (c *testClient) waitExit() int {
	c.t.Helper()
	select {
	case <-c.done:
		return c.code
	case <-time.After(responseTimeout):
		c.t.Fatal("language server did not exit")
		return 0
	}
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var value T
	assert.Success(t, json.Unmarshal(raw, &value))
	return value
}

func (c *testClient) receive(deadline <-chan time.Time) wireMessage {
	c.t.Helper()
	select {
	case raw, ok := <-c.inbox:
		if !ok {
			c.t.Fatal("server output closed while waiting for a message")
		}
		m := decode[wireMessage](c.t, raw)
		assert.Equal(c.t, "2.0", m.JSONRPC)
		return m
	case <-deadline:
		c.t.Fatal("timed out waiting for a language server message")
		return wireMessage{}
	}
}

func (c *testClient) response(method string, params any) wireMessage {
	c.t.Helper()
	c.id++
	assert.Success(c.t, c.conn.write(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params}))
	deadline := time.After(responseTimeout)
	for {
		m := c.receive(deadline)
		if m.Method != "" {
			c.pending = append(c.pending, m)
			continue
		}
		assert.Equal(c.t, strconv.Itoa(c.id), string(m.ID))
		if (m.Error == nil) == (m.Result == nil) {
			c.t.Fatalf("response to %s must have exactly one of result or error: %+v", method, m)
		}
		return m
	}
}

func (c *testClient) request(method string, params any) json.RawMessage {
	c.t.Helper()
	m := c.response(method, params)
	if m.Error != nil {
		c.t.Fatalf("%s failed: %+v", method, m.Error)
	}
	return m.Result
}

func (c *testClient) requestError(method string, params any) rpcError {
	c.t.Helper()
	m := c.response(method, params)
	if m.Error == nil {
		c.t.Fatalf("%s unexpectedly succeeded: %s", method, m.Result)
	}
	return *m.Error
}

func (c *testClient) notify(method string, params any) {
	c.t.Helper()
	assert.Success(c.t, c.conn.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}))
}

func (c *testClient) waitDiagnosticParams(uri string) publishDiagnosticsParams {
	c.t.Helper()
	matches := func(m wireMessage) bool {
		return m.Method == "textDocument/publishDiagnostics" && decode[publishDiagnosticsParams](c.t, m.Params).URI == uri
	}
	for i, m := range c.pending {
		if matches(m) {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return decode[publishDiagnosticsParams](c.t, m.Params)
		}
	}
	deadline := time.After(responseTimeout)
	for {
		m := c.receive(deadline)
		if matches(m) {
			return decode[publishDiagnosticsParams](c.t, m.Params)
		}
		c.pending = append(c.pending, m)
	}
}

func (c *testClient) waitDiagnostics(uri string) []diagnostic {
	c.t.Helper()
	return c.waitDiagnosticParams(uri).Diagnostics
}

func (c *testClient) sendRaw(body string) {
	c.t.Helper()
	_, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(body), body)
	assert.Success(c.t, err)
}

func (c *testClient) open(uri, text string) {
	c.t.Helper()
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": "d2", "version": 1, "text": text},
	})
}

func (c *testClient) change(uri string, version int, texts ...string) {
	c.t.Helper()
	changes := make([]map[string]string, 0, len(texts))
	for _, text := range texts {
		changes = append(changes, map[string]string{"text": text})
	}
	c.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": changes,
	})
}

func docParams(uri string) documentParams {
	return documentParams{TextDocument: textDocumentIdentifier{URI: uri}}
}

func posParams(uri string, line, column int) textDocumentPositionParams {
	return textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: lspPosition{line, column}}
}

func lineRange(line, start, end int) lspRange {
	return lspRange{Start: lspPosition{line, start}, End: lspPosition{line, end}}
}
