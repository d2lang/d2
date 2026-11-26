package lspserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const (
	maxHeaderBytes  = 4 << 10
	maxMessageBytes = 16 << 20
)

type conn struct {
	r  *bufio.Reader
	mu sync.Mutex // guards w
	w  io.Writer
}

// read returns the next message body.
func (c *conn) read() ([]byte, error) {
	length := -1
	headerBytes := 0
	for {
		raw, err := c.r.ReadSlice('\n')
		headerBytes += len(raw)
		if errors.Is(err, bufio.ErrBufferFull) || headerBytes > maxHeaderBytes {
			return nil, fmt.Errorf("headers exceed the %d byte limit", maxHeaderBytes)
		}
		if err != nil {
			return nil, err
		}
		line := strings.TrimRight(string(raw), "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("malformed header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	if length > maxMessageBytes {
		return nil, fmt.Errorf("message of %d bytes exceeds the %d byte limit", length, maxMessageBytes)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *conn) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

type incoming struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (m incoming) valid() bool {
	if m.JSONRPC != "2.0" {
		return false
	}
	if m.Method == "" {
		return m.ID != nil // a response, including a possible null ID on error
	}
	if m.ID == nil || m.ID[0] == '"' {
		return true // a notification or a request with a string ID
	}
	_, err := strconv.ParseInt(string(m.ID), 10, 64)
	return err == nil
}

type okResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"` // nil marshals as null, which is required
}

type errResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // nil marshals as null, for parse errors
	Error   rpcError        `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}
