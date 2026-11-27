//go:build !d2nomcp

package d2cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2lang/util-go/xmain"
)

// IOTransport treats EOF as a full disconnect and cancels outstanding calls.
// A pipe can close stdin while stdout is still open. Hold EOF until the SDK
// writes the responses, so shell pipelines get the same results as live clients.
func mcpTransport(ms *xmain.State) mcp.Transport {
	return mcpStdio{ms.Stdin, ms.Stdout}
}

type mcpStdio struct {
	input  io.Reader
	output io.WriteCloser
}

func (t mcpStdio) Connect(ctx context.Context) (mcp.Connection, error) {
	d := &mcpDrain{
		input: bufio.NewScanner(t.input), output: t.output,
		cond: sync.NewCond(&sync.Mutex{}), pending: make(map[jsonrpc.ID]bool),
	}
	d.inputCloser, _ = t.input.(io.Closer)
	d.input.Buffer(nil, mcp.DefaultMaxLineLength+1)
	transport := &mcp.IOTransport{
		Reader: d,
		Writer: struct {
			io.Writer
			io.Closer
		}{d, t.output},
	}
	conn, err := transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	// Cancellation must also interrupt a response blocked on a full stdout pipe.
	d.cond.L.Lock()
	d.abort = conn.Close
	d.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	d.cond.L.Unlock()
	return conn, nil
}

type mcpDrain struct {
	input       *bufio.Scanner
	inputCloser io.Closer
	output      io.Writer
	buffer      []byte

	cond    *sync.Cond
	pending map[jsonrpc.ID]bool
	closed  bool
	writing bool
	abort   func() error
	stop    func() bool
}

func (d *mcpDrain) Read(p []byte) (int, error) {
	if len(d.buffer) == 0 {
		if !d.input.Scan() {
			if err := d.input.Err(); err != nil {
				return 0, err
			}
			d.cond.L.Lock()
			defer d.cond.L.Unlock()
			for (len(d.pending) != 0 || d.writing) && !d.closed {
				d.cond.Wait()
			}
			return 0, io.EOF
		}
		line := d.input.Bytes()
		d.expect(line)
		d.buffer = append(line, '\n')
	}
	n := copy(p, d.buffer)
	d.buffer = d.buffer[n:]
	return n, nil
}

func (d *mcpDrain) Write(p []byte) (int, error) {
	// Forget IDs before writing: a client can reuse an ID as soon as it reads
	// the response. Keep EOF blocked until the write itself finishes.
	d.acknowledge(p)
	n, err := d.output.Write(p)
	d.cond.L.Lock()
	d.writing = false
	d.cond.Broadcast()
	d.cond.L.Unlock()
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = d.abort()
	}
	return n, err
}

func (d *mcpDrain) Close() error {
	d.cond.L.Lock()
	d.closed = true
	d.stop()
	d.cond.Broadcast()
	d.cond.L.Unlock()
	if d.inputCloser != nil {
		return d.inputCloser.Close()
	}
	return nil
}

// Decode incoming IDs with the same rules as IOTransport. A subscriptions/listen
// request remains open until the client disconnects, so it cannot delay EOF.
func (d *mcpDrain) expect(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	records := []json.RawMessage{line}
	if line[0] == '[' {
		records = nil
		if err := json.Unmarshal(line, &records); err != nil {
			return
		}
	}
	var ids []jsonrpc.ID
	for _, raw := range records {
		msg, err := jsonrpc.DecodeMessage(raw)
		if err != nil {
			return
		}
		if r, ok := msg.(*jsonrpc.Request); ok {
			// The SDK buffers legacy batch responses together. A batch
			// containing a notification or open subscription may never flush.
			if !r.IsCall() || r.Method == "subscriptions/listen" {
				return
			}
			ids = append(ids, r.ID)
		}
	}
	d.cond.L.Lock()
	defer d.cond.L.Unlock()
	for _, id := range ids {
		d.pending[id] = true
	}
}

func (d *mcpDrain) acknowledge(line []byte) {
	// Only read the envelope. A result can contain megabytes of image data.
	type envelope struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	var single [1]envelope
	replies := single[:]
	if len(line) > 0 && line[0] == '[' {
		replies = nil
		if err := json.Unmarshal(line, &replies); err != nil {
			return
		}
	} else if err := json.Unmarshal(line, &replies[0]); err != nil {
		return
	}
	d.cond.L.Lock()
	defer d.cond.L.Unlock()
	d.writing = true
	for _, r := range replies {
		if r.Method == "" {
			id, err := jsonrpc.MakeID(r.ID)
			if err == nil {
				delete(d.pending, id)
			}
		}
	}
}
