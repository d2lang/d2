package d2cli

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"

	"github.com/d2lang/d2/lib/localfile"
)

func TestTrackedFSHonorsLocalFilePolicy(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.d2")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideRoot := t.TempDir()
	outside := filepath.Join(outsideRoot, "outside.d2")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	rooted, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rooted.Close() })
	tracked := trackedFS{localFiles: rooted}
	file, err := tracked.Open(inside)
	if err != nil {
		t.Fatalf("open rooted regular file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if len(tracked.opened) != 1 || tracked.opened[0] != inside {
		t.Fatalf("tracked opens = %q, want [%q]", tracked.opened, inside)
	}

	if _, err := tracked.Open(outside); !errors.Is(err, localfile.ErrDenied) {
		t.Fatalf("outside-root Open error = %v, want localfile.ErrDenied", err)
	}
	if _, err := tracked.Open(root); err == nil {
		t.Fatal("trackedFS accepted a directory")
	}
	if len(tracked.opened) != 1 {
		t.Fatalf("failed opens were tracked: %q", tracked.opened)
	}

	var denied trackedFS
	if _, err := denied.Open(inside); !errors.Is(err, localfile.ErrDenied) {
		t.Fatalf("zero-policy Open error = %v, want localfile.ErrDenied", err)
	}

	trusted := trackedFS{localFiles: localfile.Unrestricted()}
	file, err = trusted.Open(outside)
	if err != nil {
		t.Fatalf("trusted CLI Open error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if len(trusted.opened) != 1 || trusted.opened[0] != outside {
		t.Fatalf("trusted tracked opens = %q, want [%q]", trusted.opened, outside)
	}
}

func TestWatchResolvesImportsRelativeToEntryFile(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "diagrams", "main.d2")
	dep := filepath.Join(root, "shared", "library.d2")
	if err := os.Mkdir(filepath.Dir(dep), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dep, []byte("database"), 0600); err != nil {
		t.Fatal(err)
	}
	fs := &trackedFS{localFiles: localfile.Unrestricted()}
	_, _, err := d2compiler.Compile(entry, strings.NewReader("...@../shared/library"), &d2compiler.CompileOptions{FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.opened) != 1 || fs.opened[0] != dep {
		t.Fatalf("watched imports = %v, want %v", fs.opened, dep)
	}
}

func TestVideoWatchPreviewUsesFirstFrame(t *testing.T) {
	var preview []byte
	var sent int
	scale := 2.
	emit := withVideoWatchPreview(t.Context(), &scale, &preview, func(_ image.Image, repeats int) error {
		sent += repeats
		return nil
	})
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	frame.SetNRGBA(0, 0, color.NRGBA{R: 50, A: 255})
	if err := emit(frame, 2); err != nil {
		t.Fatal(err)
	}
	frame.SetNRGBA(0, 0, color.NRGBA{B: 100, A: 255})
	if err := emit(frame, 3); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Inner struct {
			Class  string `xml:"class,attr"`
			Width  int    `xml:"width,attr"`
			Height int    `xml:"height,attr"`
			Image  struct {
				Href string `xml:"href,attr"`
			} `xml:"image"`
		} `xml:"svg"`
	}
	if err := xml.Unmarshal(preview, &document); err != nil {
		t.Fatal(err)
	}
	if sent != 5 || document.Inner.Class != "d2-svg" || document.Inner.Width != 2 || document.Inner.Height != 1 {
		t.Fatalf("sent %d frames with watch preview %+v", sent, document.Inner)
	}
	packed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(document.Inner.Image.Href, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	picture, err := png.Decode(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	if pixel := color.NRGBAModel.Convert(picture.At(0, 0)); pixel != (color.NRGBA{R: 50, A: 255}) {
		t.Fatalf("watch preview pixel = %v", pixel)
	}
}
