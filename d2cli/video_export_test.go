package d2cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func requireVideoTool(t *testing.T, available bool, message string) {
	t.Helper()
	if !available {
		if os.Getenv("D2_REQUIRE_VIDEO_TESTS") == "1" {
			t.Fatal(message)
		}
		t.Skip(message)
	}
}

func videoTestTools(t *testing.T, encoder string) (ffmpeg, ffprobe string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	requireVideoTool(t, err == nil, "ffmpeg unavailable")
	ffprobe, err = exec.LookPath("ffprobe")
	requireVideoTool(t, err == nil, "ffprobe unavailable")
	encoders, err := exec.CommandContext(t.Context(), ffmpeg, "-encoders").Output()
	requireVideoTool(t, err == nil && bytes.Contains(encoders, []byte(" "+encoder+" ")), "FFmpeg with "+encoder+" unavailable")
	return ffmpeg, ffprobe
}

func TestMP4Playback(t *testing.T) {
	testVideoPlayback(t, MP4, "libx264", "h264")
}

func TestWebMPlayback(t *testing.T) {
	testVideoPlayback(t, WEBM, "libvpx-vp9", "vp9")
}

// Decode both sides of an authored transition to check the playback clock.
func testVideoPlayback(t *testing.T, format exportExtension, encoder, codec string) {
	t.Helper()
	ffmpeg, ffprobe := videoTestTools(t, encoder)
	stdoutFormat := "--stdout-format=" + strings.TrimPrefix(string(format), ".")
	const source = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 100ms; animate-progress-color: lime; theme-overrides: {B1: purple}; pad: 0}}
swatch: "" {width: 47; height: 41; style: {stroke-width: 0; fill: red}}
steps: {next: {swatch.style.fill: blue}}`
	dir := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH")}
	output := filepath.Join(dir, "video output", "two boards"+string(format))
	_, err := imageCLIWithEnv(t, dir, source, env, "--video-quality=high", "--video-size=320x180", "--animate-progress=false", output)
	if err != nil {
		t.Fatal(err)
	}
	clip, err := imageCLIWithEnv(t, dir, source, env, stdoutFormat, "--video-duration=700ms", "-")
	if err != nil {
		t.Fatal(err)
	}
	switch format {
	case MP4:
		if moov, mdat := bytes.Index(clip, []byte("moov")), bytes.Index(clip, []byte("mdat")); moov < 0 || mdat <= moov {
			t.Fatal("stdout MP4 is missing its fast-start index")
		}
	case WEBM:
		// ffprobe reports format_name "matroska,webm" for either muxer.
		// The EBML header identifies which container was actually written.
		if !bytes.Contains(clip[:min(len(clip), 64)], []byte("\x42\x82\x84webm")) {
			t.Fatal("stdout clip is missing its WebM document type")
		}
	}
	cmd := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "format=duration", "-of", "csv=p=0", "-i", "pipe:0")
	cmd.Stdin = bytes.NewReader(clip)
	count, err := cmd.Output()
	if err != nil || string(bytes.TrimSpace(count)) != "0.700000" {
		t.Fatalf("700ms stdout clip has duration %q: %v", count, err)
	}
	probe, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-count_packets", "-show_streams", "-show_format", "-of", "json", output).Output()
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Format  struct{ Duration string }
		Streams []struct {
			Codec      string `json:"codec_name"`
			Format     string `json:"pix_fmt"`
			ColorRange string `json:"color_range"`
			ColorSpace string `json:"color_space"`
			Primaries  string `json:"color_primaries"`
			Transfer   string `json:"color_transfer"`
			Frames     string `json:"nb_read_packets"`
			Width      int
			Height     int
		}
	}
	if err := json.Unmarshal(probe, &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Streams) != 1 {
		t.Fatalf("expected a single video stream, got %d", len(info.Streams))
	}
	s := info.Streams[0]
	frames, err := strconv.Atoi(s.Frames)
	if err != nil || frames < 2 || frames >= 12 || info.Format.Duration != "0.400000" ||
		s.Codec != codec || s.Format != "yuv420p" || s.Width != 320 || s.Height != 180 ||
		s.ColorSpace != "bt709" || s.Primaries != "bt709" || s.Transfer != "iec61966-2-1" || s.ColorRange != "tv" {
		t.Fatalf("unexpected video metadata: %+v", info)
	}

	// Take the center pixel of the swatch in each decoded frame.
	samples, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", output,
		"-vf", "fps=30:start_time=0,crop=2:2,scale=1:1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-threads", "1", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 12*3 {
		t.Fatalf("decoded %d bytes, want 12 RGB pixels", len(samples))
	}
	pixel := func(frame int) []byte { return samples[3*frame : 3*frame+3] }
	red, blend, blue := pixel(0), pixel(8), pixel(11)
	if red[0] < 220 || red[2] > 25 || blend[0] < 20 || blend[2] < 20 || blue[0] > 25 || blue[2] < 220 {
		t.Fatalf("decoded colors at frames 0, 8, 11 = %v, %v, %v", red, blend, blue)
	}

	// A clip length requests a video, but its implicit 1000ms board interval
	// should not turn on the bar. Sample the same spot across each export.
	implicitSource := strings.Replace(source, "animate-interval: 200ms; ", "", 1)
	implicit, err := imageCLIWithEnv(t, dir, implicitSource, env, stdoutFormat, "--video-duration=100ms", "--video-size=320x180", "-")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		clip     []byte
		wantLime bool
	}{
		{"disabled over authored interval", nil, false},
		{"authored interval", clip, true},
		{"implicit interval", implicit, false},
	} {
		input := output
		if tc.clip != nil {
			input = "pipe:0"
		}
		decode := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", input,
			"-vf", "crop=2:2:2:ih-4,scale=1:1", "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "rgb24", "-threads", "1", "pipe:1")
		if tc.clip != nil {
			decode.Stdin = bytes.NewReader(tc.clip)
		}
		data, err := decode.Output()
		if err != nil || len(data) < 3 {
			t.Fatalf("%s: decode error %v (%d bytes)", tc.name, err, len(data))
		}
		last := data[len(data)-3:]
		lime := last[0] < 20 && last[1] > 230 && last[2] < 20
		white := last[0] > 235 && last[1] > 235 && last[2] > 235
		if tc.wantLime && !lime || !tc.wantLime && !white {
			t.Fatalf("%s: final RGB is %v, want lime when enabled and the white letterbox when disabled", tc.name, last)
		}
	}
}

func TestVideoOutputOptions(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--video-quality=ultra", "out.mp4"}, "low, medium, high, or best"},
		{[]string{"--video-quality=", "out.mp4"}, "low, medium, high, or best"},
		{[]string{"--video-quality=high", "out.svg"}, "--video-quality requires MP4 or WebM"},
		{[]string{"--video-size=1920x1080", "out.gif"}, "--video-size requires MP4 or WebM"},
		{[]string{"--video-size=1080p", "out.mp4"}, "--video-size"},
		{[]string{"--video-size=1080p", "out.webm"}, "--video-size"},
		{[]string{"--video-size=1920x1080", "--scale=2", "out.mp4"}, "--scale and --video-size"},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			data, err := imageCLI(t, t.TempDir(), "x", tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(data) != 0 {
				t.Fatalf("got %d bytes and %v, want %q", len(data), err, tc.want)
			}
		})
	}
}
