package d2cli

import (
	"context"
	"errors"
	"io"
)

// The encoded movie is ready before the first byte reaches stdout. Closing
// the writer on cancellation also wakes a transfer blocked on a full pipe.
func copyVideoStdout(ctx context.Context, dst io.WriteCloser, src io.Reader) (bool, error) {
	out, release, err := videoPipeWriter(dst)
	if err != nil {
		return false, err
	}
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		out.Close()
		close(closed)
	})
	n, err := io.Copy(rasterPNGContextWriter{ctx: ctx, output: out}, src)
	if stop() {
		err = errors.Join(err, out.Close())
	} else {
		<-closed
		err = ctx.Err()
	}
	if release != nil {
		err = errors.Join(err, release())
	}
	return n > 0, err
}
