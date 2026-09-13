//go:build nodagre

package d2cli

import (
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/util-go/xmain"
)

const dagreEnabled = false
const dagreShortHelp = ""
const dagreLongHelp = ""

func registerDagreFlags(*xmain.Opts) {}

func dagreLayout(*xmain.State) (d2graph.LayoutGraph, error) {
	return nil, layoutNotFound("dagre")
}
