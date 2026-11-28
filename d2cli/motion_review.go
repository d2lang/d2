package d2cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2lint"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/d2layoutfeatures"
)

// Both commands consume an ordinary CLI input. Only storyboard writes a file.
func motionReviewArgs(command string, args []string, motion bool) ([]string, error) {
	if command == "lint" {
		if !motion || len(args) != 1 {
			return nil, xmain.UsageErrorf("usage: d2 lint --motion file.d2|-")
		}
		return []string{args[0], "-"}, nil
	}
	if len(args) == 0 || len(args) > 2 {
		return nil, xmain.UsageErrorf("usage: d2 storyboard file.d2|- [board.png|-]")
	}
	if len(args) == 2 {
		if args[1] != "-" && !strings.EqualFold(filepath.Ext(args[1]), ".png") {
			return nil, xmain.UsageErrorf("storyboard output must be a .png file or - for stdout")
		}
		return args, nil
	}
	output := "-"
	if args[0] != "-" {
		output = renameExt(args[0], ".storyboard.png")
	}
	return []string{args[0], output}, nil
}

func motionReviewInterval(config *d2target.Config, override *int64) (int, error) {
	interval := config.ResolveAnimateInterval(override)
	if interval == nil || *interval == 0 {
		return 1000, nil // Same default as GIF playback.
	}
	if err := validateAnimateInterval(*interval, GIF); err != nil {
		return 0, err
	}
	return int(*interval), nil
}

func runMotionReview(ctx context.Context, ms *xmain.State, command, inputPath, outputPath string, layout *string, fontFamily, monoFontFamily *d2fonts.FontFamily, intervalFlag, durationFlag *int64, forceAppendixFlag *bool, renderOpts d2svg.RenderOpts) error {
	input, err := ms.ReadPath(inputPath)
	if err != nil {
		return err
	}
	opts, err := newCLICompileOptions(ctx, ms, localFilesFromContext(ctx), inputPath, layout, fontFamily, monoFontFamily, durationFlag, PNG)
	if err != nil {
		return err
	}
	opts.AnimateTweens = new(true)
	diagram, graph, err := d2lib.Compile(ctx, string(input), opts, &renderOpts)
	if err != nil {
		return err
	}
	if err := d2layoutfeatures.Check(*opts.Layout, graph); err != nil {
		return err
	}
	interval, err := motionReviewInterval(diagram.Config, intervalFlag)
	if err != nil {
		return err
	}
	if command == "lint" {
		findings, err := d2lint.Motion(ctx, diagram, graph, interval)
		if err != nil {
			return err
		}
		for _, finding := range findings {
			path := finding.Path
			if filepath.IsAbs(path) {
				path = ms.HumanPath(path)
			}
			if _, err := fmt.Fprintf(ms.Stdout, "%s:%d:%d  %s  [%s] %s\n", path, finding.Line, finding.Column, finding.Board, finding.Code, finding.Message); err != nil {
				return err
			}
		}
		if len(findings) > 0 {
			return xmain.ExitErrorf(1, "motion lint reported warnings")
		}
		return nil
	}

	plan, err := planStoryboard(ctx, diagram, interval)
	if err != nil {
		return err
	}
	paintCount := storyboardSceneCount(plan)
	assets, err := gifSceneAssetOptions(ctx, inputPath, ms.Env.Getenv("IMG_CACHE") == "1", paintCount)
	if err != nil {
		return err
	}
	defer assets.Resolver.CloseIdleConnections()
	fonts, err := newFontFallbackOptions(paintCount)
	if err != nil {
		return err
	}
	data, _, err := renderStoryboard(ctx, plan, renderOpts, diagram.Config.ResolveForceAppendix(forceAppendixFlag), assets, fonts)
	if err != nil {
		return err
	}
	return runFinalizer(ctx, func() error {
		if outputPath != "-" {
			if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
				return err
			}
		}
		return Write(ms, outputPath, data)
	})
}
