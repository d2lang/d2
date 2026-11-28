package d2cli

import (
	"bytes"
	"encoding/json"
	"image/color"
	"image/gif"
	"math"
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
		"-show_entries", "stream=width,height:format=duration", "-of", "csv=p=0", "-i", "pipe:0")
	cmd.Stdin = bytes.NewReader(clip)
	count, err := cmd.Output()
	entries := strings.Fields(string(count))
	if err != nil || len(entries) != 2 || entries[0] != "48,58" || entries[1] != "0.700000" {
		t.Fatalf("stdout clip dimensions/duration %q, want 48x58 at 700ms: %v", count, err)
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

	// Decode the gap on every pose, including transition samples. The
	// automatic 48px canvas grows by eight clear rows and a two-row line.
	gap := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", "pipe:0",
		"-vf", "crop=2:8:24:ih-10", "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "gray", "-threads", "1", "pipe:1")
	gap.Stdin = bytes.NewReader(clip)
	gapRows, err := gap.Output()
	if err != nil || len(gapRows) == 0 {
		t.Fatalf("decode progress gap: %v", err)
	}
	for _, luma := range gapRows {
		if luma < 235 {
			t.Fatalf("artwork or progress ink entered the gap: decoded gray %d", luma)
		}
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
			"-vf", "crop=2:2:2:ih-2,scale=1:1", "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "rgb24", "-threads", "1", "pipe:1")
		if tc.clip != nil {
			decode.Stdin = bytes.NewReader(tc.clip)
		}
		data, err := decode.Output()
		if err != nil || len(data) < 3 {
			t.Fatalf("%s: decode error %v (%d bytes)", tc.name, err, len(data))
		}
		last := data[len(data)-3:]
		// Chroma subsampling can soften the edge of a two-pixel line.
		lime := last[0] < 40 && last[1] > 220 && last[2] < 40
		white := last[0] > 235 && last[1] > 235 && last[2] > 235
		if tc.wantLime && !lime || !tc.wantLime && !white {
			t.Fatalf("%s: final RGB is %v, want lime when enabled and the white letterbox when disabled", tc.name, last)
		}
	}
}

func TestGradientProgressFooterMatchesSVG(t *testing.T) {
	const source = `vars.d2-config: {animate-interval: 150ms; theme-id: 200; theme-overrides: {N7: "#163050"}; pad: 4}
style.fill: "linear-gradient(to bottom, transparent, #204060)"
blank: "" {width: 112; height: 20; style: {fill: transparent; stroke-width: 0}}`
	dir := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH")}
	export := func(t *testing.T, format string, flags ...string) []byte {
		t.Helper()
		args := append([]string{"--stdout-format=" + format}, flags...)
		out, err := imageCLIWithEnv(t, dir, source, env, append(args, "-")...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	_, rest, ok := bytes.Cut(export(t, "svg"), []byte(" d2-footer-"))
	footer, _, _ := bytes.Cut(rest, []byte("</g>"))
	if !ok || !bytes.Contains(footer, []byte(`fill="#163050"`)) {
		t.Fatalf("missing the N7 override on the SVG gradient footer")
	}
	decodeGIF := func(flags ...string) *gif.GIF {
		clip, err := gif.DecodeAll(bytes.NewReader(export(t, "gif", flags...)))
		if err != nil {
			t.Fatal(err)
		}
		return clip
	}
	original, padded := decodeGIF("--animate-progress=false"), decodeGIF()
	if got := color.RGBAModel.Convert(padded.Image[0].At(2, padded.Config.Height-6)); got != (color.RGBA{0x16, 0x30, 0x50, 255}) {
		t.Fatalf("GIF gradient footer is %v", got)
	}
	for y := 0; y < original.Config.Height; y++ {
		for x := 0; x < original.Config.Width; x++ {
			if color.RGBAModel.Convert(original.Image[0].At(x, y)) != color.RGBAModel.Convert(padded.Image[0].At(x, y)) {
				t.Fatalf("GIF gradient compositing changed at (%d,%d)", x, y)
			}
		}
	}

	for _, tc := range []struct{ format, encoder string }{{"mp4", "libx264"}, {"webm", "libvpx-vp9"}} {
		t.Run(tc.format, func(t *testing.T) {
			ffmpeg, _ := videoTestTools(t, tc.encoder)
			const w, h = 160, 120
			decode := func(flag string) []byte {
				cmd := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", "pipe:0", "-frames:v", "1", "-threads", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
				cmd.Stdin = bytes.NewReader(export(t, tc.format, "--video-size=160x120", flag))
				pixels, err := cmd.Output()
				if err != nil || len(pixels) != w*h*3 {
					t.Fatalf("decode: %v (%d bytes)", err, len(pixels))
				}
				return pixels
			}
			before, after := decode("--animate-progress=false"), decode("--animate-progress=true")
			check := func(pixels []byte, x, y int, want []byte) {
				t.Helper()
				got := pixels[3*(y*w+x):][:3]
				for i := range got {
					if math.Abs(float64(got[i])-float64(want[i])) > 6 {
						t.Fatalf("at (%d,%d) got %v, want %v", x, y, got, want)
					}
				}
			}
			white := []byte{255, 255, 255}
			check(before, 80, 114, white)
			check(after, 80, 114, []byte{0x16, 0x30, 0x50})
			check(after, 80, 10, white)
			check(after, 80, 100, white)
			// This wide diagram fits above the footer without moving. Its partially
			// transparent gradient must retain the same backing in both clips.
			check(after, 80, 51, before[3*(51*w+80):][:3])
		})
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
