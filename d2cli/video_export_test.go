package d2cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise the real muxer, then decode frames on both sides of an authored
// transition. A valid MP4 header alone doesn't verify the playback clock.
func TestMP4Playback(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	encoders, err := exec.CommandContext(t.Context(), ffmpeg, "-encoders").Output()
	if err != nil || !bytes.Contains(encoders, []byte(" libx264 ")) {
		t.Skip("FFmpeg with libx264 unavailable")
	}
	const source = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 100ms; pad: 0}}
swatch: "" {width: 47; height: 41; style: {stroke-width: 0; fill: red}}
steps: {next: {swatch.style.fill: blue}}`
	dir := t.TempDir()
	env := []string{"PATH=" + os.Getenv("PATH")}
	output := filepath.Join(dir, "tour.mp4")
	_, err = imageCLIWithEnv(t, dir, source, env, output)
	if err != nil {
		t.Fatal(err)
	}
	clip, err := imageCLIWithEnv(t, dir, source, env, "--stdout-format=mp4", "--video-duration=700ms", "-")
	if err != nil {
		t.Fatal(err)
	}
	if moov, mdat := bytes.Index(clip, []byte("moov")), bytes.Index(clip, []byte("mdat")); moov < 0 || mdat <= moov {
		t.Fatal("stdout MP4 is missing its fast-start index")
	}
	cmd := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=nb_frames", "-of", "csv=p=0", "-i", "pipe:0")
	cmd.Stdin = bytes.NewReader(clip)
	count, err := cmd.Output()
	if err != nil || string(bytes.TrimSpace(count)) != "21" {
		t.Fatalf("700ms stdout clip has frame count %q: %v", count, err)
	}
	probe, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-show_streams", "-of", "json", output).Output()
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Format string `json:"pix_fmt"`
			FPS    string `json:"avg_frame_rate"`
			Frames string `json:"nb_frames"`
			Width  int
			Height int
		}
	}
	if err := json.Unmarshal(probe, &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Streams) != 1 {
		t.Fatalf("expected a single video stream, got %d", len(info.Streams))
	}
	s := info.Streams[0]
	if s.Codec != "h264" || s.Format != "yuv420p" || s.FPS != "30/1" || s.Frames != "12" ||
		s.Width%2 != 0 || s.Height%2 != 0 {
		t.Fatalf("unexpected video stream: %+v", s)
	}

	// Take the center pixel of the swatch in each decoded frame.
	samples, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", output,
		"-vf", "crop=2:2,scale=1:1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-threads", "1", "pipe:1").Output()
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
}
