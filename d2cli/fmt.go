package d2cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/d2lang/util-go/xdefer"
	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/lib/log"
)

func fmtCmd(ctx context.Context, ms *xmain.State, check, showDiff bool) (err error) {
	defer xdefer.Errorf(&err, "failed to fmt")

	paths := ms.Opts.Flags.Args()[1:]
	if len(paths) == 0 {
		return xmain.UsageErrorf("fmt must be passed at least one file to be formatted")
	}

	unformattedCount := 0

	for _, inputPath := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if inputPath != "-" {
			inputPath = ms.AbsPath(inputPath)
			d, err := os.Stat(inputPath)
			if err == nil && d.IsDir() {
				inputPath = filepath.Join(inputPath, "index.d2")
			}
		}

		input, err := ms.ReadPath(inputPath)
		if err != nil {
			return err
		}

		output, err := formatSource(ctx, inputPath, input)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		changed := !bytes.Equal(output, input)
		if check || showDiff {
			if !changed {
				continue
			}
			if showDiff {
				if err := writeFormatDiff(ms, inputPath, input, output); err != nil {
					return err
				}
			}
			if check {
				unformattedCount++
				if !showDiff {
					log.Warn(ctx, inputPath)
				}
			}
			continue
		}
		if inputPath == "-" {
			if _, err := ms.Stdout.Write(output); err != nil {
				return err
			}
		} else if changed {
			if err := ms.WritePath(inputPath, output); err != nil {
				return err
			}
		}
	}

	if unformattedCount > 0 {
		pluralFiles := "file"
		if unformattedCount > 1 {
			pluralFiles = "files"
		}

		return xmain.ExitErrorf(1, "found %d unformatted %s. Run d2 fmt to fix.", unformattedCount, pluralFiles)
	}

	return nil
}

func formatSource(ctx context.Context, path string, input []byte) ([]byte, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return d2format.FormatMarkdown(ctx, path, input)
	}
	parsed, err := d2parser.ParseContext(ctx, path, bytes.NewReader(input), nil)
	if err != nil {
		return nil, err
	}
	return []byte(d2format.Format(parsed)), nil
}

func writeFormatDiff(ms *xmain.State, path string, input, output []byte) error {
	if path == "-" {
		path = "stdin"
	} else if relative, err := filepath.Rel(ms.PWD, path); err == nil {
		path = relative
	}
	path = filepath.ToSlash(path)
	before, after := "a/"+path, "b/"+path
	if strings.ContainsAny(path, "\t\r\n\"\\") {
		before, after = strconv.Quote(before), strconv.Quote(after)
	}
	original := string(input)
	edits := udiff.Lines(original, string(output))
	patch, err := udiff.ToUnified(before, after, original, edits, udiff.DefaultContextLines)
	if err != nil {
		return err
	}
	_, err = io.WriteString(ms.Stdout, patch)
	return err
}
