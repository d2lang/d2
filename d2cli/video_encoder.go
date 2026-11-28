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

// The MP4 muxer seeks when moving its index to the front of the file. Stage
// even stdout so pipes and redirected output get the same seekable movie.
func writeVideoWithStatus(ctx context.Context, ms *xmain.State, outputPath string, quality videoQuality, render func(emit func(image.Image, int) error) error) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return false, fmt.Errorf("MP4 export requires FFmpeg 5.1 or newer with libx264 on PATH: %w", err)
	}

	dir := ""
	if outputPath != "-" {
		dir = filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
	}
	staged, err := os.CreateTemp(dir, "d2-mp4-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(staged.Name())
	if err := staged.Close(); err != nil {
		return false, err
	}
	if err := encodeVideo(ctx, ffmpeg, staged.Name(), quality, render); err != nil {
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

// The zero value uses the same high-quality setting as an ordinary CLI export.
func (q videoQuality) crf() (int, bool) {
	switch q {
	case "low":
		return 28, true
	case "medium":
		return 22, true
	case "", "high":
		return 18, true
	case "best":
		return 12, true
	default:
		return 0, false
	}
}

func encodeVideo(ctx context.Context, ffmpeg, path string, quality videoQuality, render func(func(image.Image, int) error) error) error {
	crf, ok := quality.crf()
	if !ok {
		return fmt.Errorf("invalid MP4 quality %q", quality)
	}
	encoderCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cmd *exec.Cmd
	var input io.WriteCloser
	var diagnostics videoEncoderDiagnostics
	var size image.Point
	var tick int
	renderErr := render(func(frame image.Image, repeats int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if frame == nil || repeats <= 0 {
			return fmt.Errorf("invalid MP4 frame or repeat count %d", repeats)
		}
		dimensions := frame.Bounds().Size()
		if dimensions.X <= 0 || dimensions.Y <= 0 || dimensions.X%2 != 0 || dimensions.Y%2 != 0 {
			return fmt.Errorf("MP4 frame dimensions must be positive and even, got %v", dimensions)
		}
		if cmd == nil {
			size = dimensions
			cmd = exec.CommandContext(encoderCtx, ffmpeg,
				"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-filter_threads", "1",
				"-f", "matroska", "-i", "pipe:0",
				// Preserve presentation order. With reordered B-frames the MP4
				// sample durations can land on neighboring pictures at a long hold.
				"-an", "-c:v", "libx264", "-threads", "4", "-bf", "0", "-preset", "medium", "-crf", strconv.Itoa(crf),
				// Bound the number of UHD pictures held for rate control.
				"-x264-params", "rc-lookahead=4:sync-lookahead=0",
				// Tag the converted frames themselves; output metadata alone can
				// lose the sRGB transfer function at the filter/encoder boundary.
				"-vf", "scale=in_range=full:out_range=limited:out_color_matrix=bt709,format=yuv420p,"+
					"setparams=range=limited:color_primaries=bt709:color_trc=iec61966-2-1:colorspace=bt709",
				"-enc_time_base", fmt.Sprintf("1:%d", videoFPS), "-fps_mode", "passthrough",
				"-movie_timescale", "30000", "-movflags", "+faststart", "-f", "mp4", "file:"+path)
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
			return fmt.Errorf("MP4 frame dimensions changed from %v to %v", size, dimensions)
		}
		writePose := func(hold int) error {
			if _, err := input.Write(videoBlockHeader(tick, hold, size.X*size.Y*4)); err != nil {
				return err
			}
			tick += hold
			return writeVideoFrame(input, frame)
		}
		if repeats > 1 {
			// Some FFmpeg versions discard the last packet's input duration.
			// Stamp both endpoints of a hold while we still own its pixels:
			// even those encoders can infer its entire length from timestamps.
			if err := writePose(repeats - 1); err != nil {
				return err
			}
		}
		return writePose(1)
	})

	if cmd == nil || cmd.Process == nil {
		if renderErr != nil {
			return renderErr
		}
		return errors.New("MP4 renderer produced no frames")
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
			message += " (MP4 export requires FFmpeg 5.1 or newer)"
		}
		return errors.Join(renderErr, fmt.Errorf("FFmpeg (libx264): %w: %s", waitErr, message))
	}
	if renderErr != nil {
		return renderErr
	}
	if waitErr != nil {
		return fmt.Errorf("FFmpeg (libx264): %w", waitErr)
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

// Preserve startup errors (e.g. a build without libx264) without buffering an
// unbounded amount of subprocess output.
type videoEncoderDiagnostics struct{ strings.Builder }

func (d *videoEncoderDiagnostics) Write(p []byte) (int, error) {
	const maxDiagnosticBytes = 8192
	d.Builder.Write(p[:min(len(p), maxDiagnosticBytes-d.Len())])
	return len(p), nil
}
