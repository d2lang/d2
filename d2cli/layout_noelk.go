//go:build noelk

package d2cli

import (
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/util-go/xmain"
)

const elkEnabled = false
const elkShortHelp = ""
const elkLongHelp = ""

func registerELKFlags(*xmain.Opts) {}

func elkLayout(*xmain.State) (d2graph.LayoutGraph, error) {
	return nil, layoutNotFound("elk")
}
