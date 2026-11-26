package d2cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2lsp/lspserver"
	"github.com/d2lang/d2/lib/version"
)

func lspCmd(ctx context.Context, ms *xmain.State, args []string) error {
	opts := xmain.NewOpts(ms.Env, args)
	opts.Flags.Bool("stdio", false, "use stdin and stdout (the default and only transport)")
	err := opts.Flags.Parse(args)
	if errors.Is(err, pflag.ErrHelp) {
		_, err = fmt.Fprintf(ms.Stdout, "Usage:\n  %s lsp [--stdio]\n\nRuns a Language Server Protocol server on stdin and stdout.\n", filepath.Base(ms.Name))
		return err
	}
	if err != nil {
		return xmain.UsageErrorf("failed to parse flags: %v", err)
	}
	if opts.Flags.NArg() != 0 {
		return xmain.UsageErrorf("lsp takes no arguments")
	}

	server := lspserver.New(lspserver.Options{
		In:      ms.Stdin,
		Out:     ms.Stdout,
		Log:     ms.Stderr,
		Version: version.Version,
	})
	if code := server.Run(ctx); code != 0 {
		return xmain.ExitErrorf(code, "language server exited before completing shutdown")
	}
	return nil
}
