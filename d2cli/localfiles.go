package d2cli

import (
	"context"

	"github.com/d2lang/d2/lib/localfile"
)

type localFilesKey struct{}

// withLocalFiles sets the policy used for every local file the diagram reads:
// imports, and images bundled into or rendered in the output.
func withLocalFiles(ctx context.Context, policy localfile.Policy) context.Context {
	return context.WithValue(ctx, localFilesKey{}, policy)
}

// localFilesFromContext returns the policy set by withLocalFiles. Without one,
// it returns an unrestricted policy, the historical behavior of the CLI.
func localFilesFromContext(ctx context.Context) localfile.Policy {
	if policy, ok := ctx.Value(localFilesKey{}).(localfile.Policy); ok {
		return policy
	}
	return localfile.Unrestricted()
}
