package d2cli

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/util-go/xmain"
)

func TestVideoFramePackedPixels(t *testing.T) {
	// Restrict the canvas so each row is separated by unused bytes in the
	// backing image. The MP4 stream must contain only the visible pixels.
	region := image.Rect(3, 1, 5, 3)
	rgba := image.NewRGBA(image.Rect(0, 0, 6, 4)).SubImage(region).(*image.RGBA)
	nrgba := image.NewNRGBA(image.Rect(0, 0, 6, 4)).SubImage(region).(*image.NRGBA)
	packed := image.NewNRGBA(region)
	palette := color.Palette{
		color.NRGBA{R: 255, A: 255}, color.NRGBA{G: 255, A: 255},
		color.NRGBA{B: 255, A: 255}, color.NRGBA{R: 123, G: 45, B: 67, A: 255},
	}
	indexed := image.NewPaletted(region, palette)
	want := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 123, 45, 67, 255}
	for i, c := range palette {
		x, y := region.Min.X+i%2, region.Min.Y+i/2
		rgba.Set(x, y, c)
		nrgba.Set(x, y, c)
		packed.Set(x, y, c)
		indexed.SetColorIndex(x, y, uint8(i))
	}
	for _, frame := range []image.Image{rgba, nrgba, packed, indexed} {
		var got bytes.Buffer
		if err := writeVideoFrame(&got, frame, 2); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), bytes.Repeat(want, 2)) {
			t.Fatalf("%T packed pixels = %x", frame, got.Bytes())
		}
	}
}

func TestVideoMissingFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.mp4")
	written, err := writeVideoWithStatus(context.Background(), &xmain.State{}, path, func(func(image.Image, int) error) error {
		t.Fatal("render started before checking FFmpeg")
		return nil
	})
	if written || err == nil || !strings.Contains(err.Error(), "FFmpeg with libx264 on PATH") {
		t.Fatalf("written %v, error %v", written, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("unexpected output: %v, %v", entries, err)
	}
}

func TestVideoEncoderFailuresPreserveDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub for FFmpeg")
	}
	renderErr := errors.New("animation failed")
	for _, tc := range []struct {
		name    string
		script  string
		want    error
		message string
		cancel  bool
	}{
		{name: "missing codec", script: "echo 'Unknown encoder libx264' >&2; exit 1", message: "Unknown encoder libx264"},
		{name: "render failure", script: "exec /bin/sleep 30", want: renderErr},
		{name: "canceled", script: "exec /bin/sleep 30", want: context.Canceled, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			dir := t.TempDir()
			path := filepath.Join(dir, "movie.mp4")
			if err := os.WriteFile(path, []byte("previous"), 0o644); err != nil {
				t.Fatal(err)
			}
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			written, err := writeVideoWithStatus(ctx, &xmain.State{}, path, func(emit func(image.Image, int) error) error {
				if err := emit(image.NewNRGBA(image.Rect(0, 0, 2, 2)), 1); err != nil {
					return err
				}
				if tc.cancel {
					cancel()
				} else if tc.want != nil {
					return tc.want
				}
				return nil
			})
			if written || err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("written %v, error %v", written, err)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != "previous" {
				t.Fatalf("destination = %q, %v", got, err)
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
				t.Fatalf("temporary file leaked: %v, %v", entries, err)
			}
		})
	}
}
