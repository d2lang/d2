//go:build d2nomcp

package d2cli

import (
	"context"

	"github.com/d2lang/util-go/xmain"
)

func mcpCmd(ctx context.Context, ms *xmain.State, args []string) error {
	return xmain.UsageErrorf("this build of d2 does not include the MCP server")
}
