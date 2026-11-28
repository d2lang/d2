package d2cli

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/d2lang/util-go/xmain"
)

// Both muxers seek to finalize their index and duration. Stage stdout too so
// a redirected export has the same metadata as a named file.
func writeVideoWithStatus(ctx context.Context, ms *xmain.State, outputPath string, format exportExtension, quality videoQuality, render func(emit func(image.Image, int) error) error) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return false, fmt.Errorf("%s export requires FFmpeg 5.1 or newer with %s on PATH: %w", format, videoEncoders[format].name, err)
	}

	dir := ""
	if outputPath != "-" {
		dir = filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
	}
	staged, err := os.CreateTemp(dir, "d2-video-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(staged.Name())
	if err := staged.Close(); err != nil {
		return false, err
	}
	if err := encodeVideo(ctx, ffmpeg, staged.Name(), format, quality, render); err != nil {
		return false, err
	}
	return runStatusFinalizer(ctx, func() (bool, error) {
		if outputPath != "-" {
			err := os.Rename(staged.Name(), outputPath)
			return err == nil, err
		}

		source, err := os.Open(staged.Name())
		if err != nil {
			return false, err
		}
		defer source.Close()
		return copyVideoStdout(ctx, ms.Stdout, source)
	})
}

type videoQuality string

// Each codec uses its own constant-quality scale.
var videoEncoders = map[exportExtension]struct {
	name string
	crf  map[videoQuality]int
}{
	MP4:  {"libx264", map[videoQuality]int{"low": 28, "medium": 22, "high": 18, "best": 12}},
	WEBM: {"libvpx-vp9", map[videoQuality]int{"low": 48, "medium": 42, "high": 36, "best": 20}},
}

func (q videoQuality) crf(format exportExtension) (int, bool) {
	if q == "" {
		q = "high"
	}
	crf, ok := videoEncoders[format].crf[q]
	return crf, ok
}

func videoEncoderArgs(format exportExtension, crf int, path string) []string {
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-filter_threads", "1",
		"-f", "matroska", "-i", "pipe:0", "-an",
		"-c:v", videoEncoders[format].name, "-threads", "4", "-crf", strconv.Itoa(crf),
		// Tag the converted frames themselves so the sRGB transfer function
		// survives the filter/encoder boundary.
		"-vf", "scale=in_range=full:out_range=limited:out_color_matrix=bt709,format=yuv420p," +
			"setparams=range=limited:color_primaries=bt709:color_trc=iec61966-2-1:colorspace=bt709",
		"-enc_time_base", fmt.Sprintf("1:%d", videoFPS), "-fps_mode", "passthrough",
	}
	switch format {
	case MP4:
		// Reordered B-frames can shift a long hold onto the next picture.
		// Bound the number of UHD pictures held for rate control.
		args = append(args, "-bf", "0", "-preset", "medium", "-x264-params", "rc-lookahead=4:sync-lookahead=0",
			"-movie_timescale", "30000", "-movflags", "+faststart", "-f", "mp4")
	case WEBM:
		// Screen tuning preserves diagram edges. Encode in one pass without
		// queuing future UHD frames for lookahead.
		args = append(args, "-b:v", "0", "-deadline", "realtime", "-cpu-used", "5", "-tune-content", "screen",
			"-row-mt", "1", "-lag-in-frames", "0", "-f", "webm")
	}
	return append(args, "file:"+path)
}

func encodeVideo(ctx context.Context, ffmpeg, path string, format exportExtension, quality videoQuality, render func(func(image.Image, int) error) error) error {
	crf, ok := quality.crf(format)
	if !ok {
		return fmt.Errorf("invalid video format %q or quality %q", format, quality)
	}
	encoderCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cmd *exec.Cmd
	var input io.WriteCloser
	var diagnostics videoEncoderDiagnostics
	var size image.Point
	var tick, lastHold int
	renderErr := render(func(frame image.Image, repeats int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if frame == nil || repeats <= 0 {
			return fmt.Errorf("invalid video frame or repeat count %d", repeats)
		}
		dimensions := frame.Bounds().Size()
		if dimensions.X <= 0 || dimensions.Y <= 0 || dimensions.X%2 != 0 || dimensions.Y%2 != 0 {
			return fmt.Errorf("video frame dimensions must be positive and even, got %v", dimensions)
		}
		if cmd == nil {
			size = dimensions
			cmd = exec.CommandContext(encoderCtx, ffmpeg, videoEncoderArgs(format, crf, path)...)
			cmd.Stderr = &diagnostics
			cmd.WaitDelay = 2 * time.Second
			var err error
			input, err = cmd.StdinPipe()
			if err != nil {
				return err
			}
			if err := cmd.Start(); err != nil {
				return fmt.Errorf("start FFmpeg: %w", err)
			}
			if _, err := input.Write(videoTrackHeader(size)); err != nil {
				return err
			}
		} else if dimensions != size {
			return fmt.Errorf("video frame dimensions changed from %v to %v", size, dimensions)
		}
		if _, err := input.Write(videoBlockHeader(tick, repeats, size.X*size.Y*4)); err != nil {
			return err
		}
		tick += repeats
		lastHold = repeats
		return writeVideoFrame(input, frame)
	})

	if cmd == nil || cmd.Process == nil {
		if renderErr != nil {
			return renderErr
		}
		return errors.New("video renderer produced no frames")
	}

	if renderErr == nil && lastHold != 1 {
		renderErr = errors.New("video renderer must emit its final tick separately")
	}
	closeErr := input.Close()
	if renderErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if message := strings.TrimSpace(diagnostics.String()); waitErr != nil && message != "" {
		if strings.Contains(message, "fps_mode") {
			message += " (video export requires FFmpeg 5.1 or newer)"
		}
		return errors.Join(renderErr, fmt.Errorf("FFmpeg (%s): %w: %s", videoEncoders[format].name, waitErr, message))
	}
	if renderErr != nil {
		return renderErr
	}
	if waitErr != nil {
		return fmt.Errorf("FFmpeg (%s): %w", videoEncoders[format].name, waitErr)
	}
	return closeErr
}

// Frames produced by the rasterizer are opaque, so RGBA and NRGBA share
// their on-wire representation. Subimages may have padding between rows.
func writeVideoFrame(output io.Writer, frame image.Image) error {
	bounds := frame.Bounds()
	var pixels []byte
	var stride int
	switch img := frame.(type) {
	case *image.NRGBA:
		pixels, stride = img.Pix, img.Stride
	case *image.RGBA:
		pixels, stride = img.Pix, img.Stride
	default:
		converted := image.NewNRGBA(bounds)
		draw.Draw(converted, bounds, frame, bounds.Min, draw.Src)
		pixels, stride = converted.Pix, converted.Stride
	}
	rowBytes := 4 * bounds.Dx()
	if stride == rowBytes {
		_, err := output.Write(pixels[:rowBytes*bounds.Dy()])
		return err
	}
	for y := range bounds.Dy() {
		if _, err := output.Write(pixels[y*stride : y*stride+rowBytes]); err != nil {
			return err
		}
	}
	return nil
}

// Preserve startup errors (e.g. an unavailable encoder) without buffering an
// unbounded amount of subprocess output.
type videoEncoderDiagnostics struct{ strings.Builder }

func (d *videoEncoderDiagnostics) Write(p []byte) (int, error) {
	const maxDiagnosticBytes = 8192
	d.Builder.Write(p[:min(len(p), maxDiagnosticBytes-d.Len())])
	return len(p), nil
}
