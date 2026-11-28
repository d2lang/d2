package d2cli

import (
	"bytes"
	"context"
	"fmt"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/util-go/xdefer"
	"github.com/d2lang/util-go/xmain"
)

func validateCmd(ctx context.Context, ms *xmain.State) (err error) {
	defer xdefer.Errorf(&err, "")

	ms.Opts = xmain.NewOpts(ms.Env, ms.Opts.Flags.Args()[1:])
	if len(ms.Opts.Args) == 0 {
		return xmain.UsageErrorf("input argument required")
	}

	inputPath := ms.Opts.Args[0]
	if inputPath != "-" {
		inputPath = ms.AbsPath(inputPath)
	}

	input, err := ms.ReadPath(inputPath)
	if err != nil {
		return err
	}

	_, _, err = d2compiler.Compile(inputPath, bytes.NewReader(input), &d2compiler.CompileOptions{Context: ctx, FS: localFilesFromContext(ctx)})
	if err != nil {
		return err
	}

	if inputPath == "-" {
		inputPath = "Input"
	}

	fmt.Fprintf(ms.Stdout, "Success! [%s] is valid D2.\n", inputPath)
	return nil
}
