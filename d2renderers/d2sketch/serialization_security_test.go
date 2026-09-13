package d2sketch

import (
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

func TestConnectionRejectsConflictingAttributesWithoutPanic(t *testing.T) {
	t.Parallel()

	for _, attributes := range []string{
		`class="override"`,
		`d="M 9 9"`,
		`style="fill:red"`,
		`mask="url(#one)" mask="url(#two)"`,
	} {
		if _, err := Connection(d2target.Connection{}, "M 0 0 L 1 1", attributes); err == nil {
			t.Errorf("Connection accepted conflicting attributes %q", attributes)
		}
	}

	if _, err := ConnectionWithAttributes(
		d2target.Connection{},
		"M 0 0 L 1 1",
		[]svg.Attribute{svg.Attr("mask", "url(#one)"), svg.Attr("mask", "url(#two)")},
	); err == nil {
		t.Error("ConnectionWithAttributes accepted duplicate typed attributes")
	}
	if _, err := ConnectionWithAttributes(d2target.Connection{}, "M 0 0 L 1 1", []svg.Attribute{{}}); err == nil {
		t.Error("ConnectionWithAttributes accepted a zero-value attribute")
	}
}
