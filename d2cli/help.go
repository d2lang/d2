package d2cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/version"
)

func help(ms *xmain.State) {
	fmt.Fprintf(ms.Stdout, `%[1]s %[2]s
Usage:
  %[1]s [--watch=false] [--theme=0] file.d2 [file.svg | file.png | file.pdf | file.pptx | file.gif | file.txt]
  %[1]s layout [name]
  %[1]s fmt file.d2 ...
  %[1]s play [--theme=0] [--sketch] file.d2
  %[1]s validate file.d2

%[1]s compiles and renders file.d2 to file.svg | file.png | file.pdf | file.pptx | file.gif | file.txt
It defaults to file.svg if an output path is not provided.

Use - to have d2 read from stdin or write to stdout.

PNG exports support up to 32768 pixels per dimension, subject to rendering resource limits.

See man d2 for more detailed docs.

Flags:
%[3]s

Subcommands:
  %[1]s layout - Lists available layout engine options with short help
  %[1]s layout [name] - Display long help for a particular layout engine, including its configuration options
  %[1]s themes - Lists available themes
  %[1]s fmt file.d2 ... - Format passed files
	%[1]s play file.d2 - Opens the file in playground, an online web viewer (https://play.d2lang.com)
  %[1]s validate file.d2  - Validates file.d2

See more docs and the source code at https://github.com/d2lang/d2.
Hosted icons at https://icons.d2lang.com.
Playground runner at https://play.d2lang.com.
`, filepath.Base(ms.Name), version.Version, ms.Opts.Defaults())
}

func layoutCmd(ctx context.Context, ms *xmain.State) error {
	if len(ms.Opts.Flags.Args()) == 1 {
		return shortLayoutHelp(ctx, ms)
	} else if len(ms.Opts.Flags.Args()) == 2 {
		return longLayoutHelp(ctx, ms)
	} else {
		return xmain.UsageErrorf("layout subcommand accepts at most one argument")
	}
}

func themesCmd(_ context.Context, ms *xmain.State) {
	fmt.Fprintf(ms.Stdout, "Available themes:\n%s", d2themescatalog.CLIString())
}

func shortLayoutHelp(ctx context.Context, ms *xmain.State) error {
	var layoutLines []string
	for _, name := range builtinLayoutNames() {
		shortHelp, _ := builtinLayoutHelp(name)
		layoutLines = append(layoutLines, fmt.Sprintf("%s (built-in) - %s", name, shortHelp))
	}
	fmt.Fprintf(ms.Stdout, `Available layout engines:

%s

Usage:
  To use a particular layout engine, set the environment variable D2_LAYOUT=[name] or flag --layout=[name].

Example:
  D2_LAYOUT=dagre d2 in.d2 out.svg

Subcommands:
  %s layout [layout name] - Display long help for a particular layout engine, including its configuration options

See more docs at https://d2lang.com/tour/layouts
`, strings.Join(layoutLines, "\n"), ms.Name)
	return nil
}

func longLayoutHelp(ctx context.Context, ms *xmain.State) error {
	name := strings.ToLower(ms.Opts.Flags.Arg(1))
	if !isBuiltinLayout(name) {
		return layoutNotFound(name)
	}
	_, longHelp := builtinLayoutHelp(name)
	fmt.Fprintf(ms.Stdout, "%s (built-in):\n\n%s", name, longHelp)
	return nil
}

func builtinLayoutHelp(name string) (shortHelp, longHelp string) {
	opts := xmain.NewOpts(nil, nil)
	switch name {
	case "dagre":
		shortHelp, longHelp = dagreShortHelp, dagreLongHelp
		registerDagreFlags(opts)
	case "elk":
		shortHelp, longHelp = elkShortHelp, elkLongHelp
		registerELKFlags(opts)
	case "tala":
		shortHelp, longHelp = talaShortHelp, talaLongHelp
		registerTALAFlags(opts)
	}
	return shortHelp, longHelp + "\nFlags:\n" + opts.Defaults() + "\n"
}

func layoutNotFound(name string) error {
	return xmain.UsageErrorf(`D2_LAYOUT "%s" is not a supported built-in layout engine.
The available options are: %s. For details on each option, run "d2 layout".
External d2plugin executables are no longer supported.`, name, strings.Join(builtinLayoutNames(), ", "))
}
