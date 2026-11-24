package d2cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/d2lang/util-go/xmain"
)

const issueReportURL = "https://github.com/d2lang/d2/issues"

func runWithPanicRecovery(ctx context.Context, ms *xmain.State, fn func(context.Context, *xmain.State) error) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		var w io.Writer = os.Stderr
		if ms != nil && ms.Stderr != nil {
			w = ms.Stderr
		}
		fmt.Fprintf(w, "d2 panicked: %v\n\nPlease report this bug at %s\n\n%s", r, issueReportURL, debug.Stack())
		err = xmain.ExitError{Code: 2}
	}()
	return fn(ctx, ms)
}
