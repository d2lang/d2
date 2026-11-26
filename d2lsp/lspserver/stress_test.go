package lspserver

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"
)

func TestL75_InterleavedChangesAndHovers(t *testing.T) {
	const uri = "untitled:interleaved"
	c := newTestClient(t)
	c.open(uri, "x: initial")
	c.waitDiagnostics(uri)
	for version := 2; version < 52; version++ {
		label := fmt.Sprintf("version %d", version)
		c.change(uri, version, "x: "+label)
		// Schedule some immediate compiles to overlap requests and further edits.
		if version%5 == 0 {
			c.notify("textDocument/didSave", docParams(uri))
		}
		got := decode[*hover](t, c.request("textDocument/hover", posParams(uri, 0, 0)))
		if got == nil || !strings.Contains(got.Contents.Value, "label: "+label+"\n") {
			t.Fatalf("hover for version %d: %+v", version, got)
		}
	}
	for {
		publish := c.waitDiagnosticParams(uri)
		if publish.Version == 51 {
			assert.JSON(t, []diagnostic{}, publish.Diagnostics)
			break
		}
	}
}

func FuzzL76_FrameReader(f *testing.F) {
	for _, frame := range []string{
		"", "Content-Length: 0\r\n\r\n",
		"Content-Length: 2\r\n\r\n{}",
		"content-length: 4\nContent-Type: application/json\n\n😀",
		"Content-Length: -1\r\n\r\n", "Content-Length: 16777217\r\n\r\n",
		"Content-Length: 12\r\n\r\nshort", "no colon\n\n{}",
	} {
		f.Add([]byte(frame))
	}
	f.Fuzz(func(t *testing.T, frame []byte) {
		c := conn{r: bufio.NewReader(bytes.NewReader(frame))}
		body, err := c.read()
		if err == nil && (len(body) > maxMessageBytes || len(body) > len(frame)) {
			t.Fatalf("read %d bytes from %d-byte frame", len(body), len(frame))
		}
	})
}
