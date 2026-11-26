package asciicanvas

import (
	"context"
	"errors"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
)

func TestConnectionLabelRowsShareCanvasBudget(t *testing.T) {
	canvas := New(4, 2)
	canvas.DrawLabelAbove(0, 0, "a\nb")
	chars := charset.New(charset.Unicode)
	if _, err := canvas.ToByteArrayContext(t.Context(), chars, 15); err == nil {
		t.Fatal("accepted a 16-cell canvas with a 15-cell limit")
	}
	got, err := canvas.ToByteArrayContext(t.Context(), chars, 16)
	if err != nil || string(got) != string(canvas.ToByteArray(chars)) {
		t.Fatalf("full-budget output = %q, %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := canvas.ToByteArrayContext(ctx, chars, 16); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled canvas serialization returned %v", err)
	}
}
