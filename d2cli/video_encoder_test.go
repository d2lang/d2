package d2cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
		if err := writeVideoFrame(&got, frame); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("%T packed pixels = %x", frame, got.Bytes())
		}
	}
}

func TestVideoMissingFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.mp4")
	written, err := writeVideoWithStatus(context.Background(), &xmain.State{}, path, "high", func(func(image.Image, int) error) error {
		t.Fatal("render started before checking FFmpeg")
		return nil
	})
	if written || err == nil || !strings.Contains(err.Error(), "FFmpeg 5.1 or newer with libx264 on PATH") {
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
			written, err := writeVideoWithStatus(ctx, &xmain.State{}, path, "high", func(emit func(image.Image, int) error) error {
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

func TestVideoQualityCRF(t *testing.T) {
	for quality, want := range map[videoQuality]int{"": 18, "low": 28, "medium": 22, "high": 18, "best": 12} {
		if got, ok := quality.crf(); !ok || got != want {
			t.Errorf("%q: CRF %d, %v; want %d", quality, got, ok, want)
		}
	}
	if _, ok := videoQuality("unknown").crf(); ok {
		t.Fatal("accepted unknown video quality")
	}
}

// Check timestamps and lengths of actual MP4 samples, including the last
// sample. A valid duration in the container header alone can mask misplaced
// packet durations in a movie with long holds and short intervening motion.
func TestVideoTimedSamples(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	requireVideoTool(t, err == nil, "ffmpeg unavailable")
	ffprobe, err := exec.LookPath("ffprobe")
	requireVideoTool(t, err == nil, "ffprobe unavailable")
	encoders, err := exec.CommandContext(t.Context(), ffmpeg, "-encoders").Output()
	requireVideoTool(t, err == nil && bytes.Contains(encoders, []byte(" libx264 ")), "FFmpeg with libx264 unavailable")
	for _, tc := range []struct {
		name             string
		holds, durations []int
	}{
		{"one tick", []int{1}, []int{1}},
		{"one static minute", []int{1800}, []int{1799, 1}},
		{"cuts and holds", []int{1800, 1, 1, 90, 1, 2, 300}, []int{1800, 1, 1, 90, 1, 2, 299, 1}},
		{"constant frame rate", []int{1, 1, 1, 1, 1}, []int{1, 1, 1, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "samples.mp4")
			if err := encodeVideo(t.Context(), ffmpeg, path, "high", func(emit func(image.Image, int) error) error {
				frame := image.NewNRGBA(image.Rect(0, 0, 64, 48))
				for i, hold := range tc.holds {
					for j := 0; j < len(frame.Pix); j += 4 {
						frame.Pix[j], frame.Pix[j+3] = byte(i*39), 255
					}
					if err := emitVideoHold(frame, hold, i == len(tc.holds)-1, emit); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			data, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=time_base:packet=pts,duration", "-of", "json", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			var meta struct {
				Streams []struct {
					Base string `json:"time_base"`
				}
				Packets []struct{ PTS, Duration int }
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatal(err)
			}
			if len(meta.Streams) != 1 || len(meta.Packets) != len(tc.durations) {
				t.Fatalf("unexpected sample table: %s", data)
			}
			base, ok := strings.CutPrefix(meta.Streams[0].Base, "1/")
			scale, err := strconv.Atoi(base)
			if !ok || err != nil || scale%videoFPS != 0 {
				t.Fatalf("invalid video timescale %q", meta.Streams[0].Base)
			}
			step := scale / videoFPS
			pts := 0
			for i, duration := range tc.durations {
				packet := meta.Packets[i]
				if packet.PTS != pts || packet.Duration != duration*step {
					t.Fatalf("packet %d: PTS %d duration %d, want PTS %d duration %d", i, packet.PTS, packet.Duration, pts, duration*step)
				}
				pts += duration * step
			}
		})
	}
}
