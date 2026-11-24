package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/graphjson"
)

// These compact goldens cover complete placement, routing, labeling, and
// serialization results for representative algorithm domains at seeds beyond
// the main layout golden's seed. An intentional output change must review and
// update the corresponding digest.
func TestLayoutOutputAcrossSeeds(t *testing.T) {
	testCases := []struct {
		fixture string
		seed    int64
		sha256  string
	}{
		{fixture: "clusters", seed: 1, sha256: "bcf99447349a3053b7eafd0fd257e8462aa41b8583a4f6274a3d820fb20b986c"},
		{fixture: "clusters", seed: 123, sha256: "a7c8a0c80712a10754d0925c84ccd6cd880bb7497fefca69c86a286fb19cd7d1"},
		{fixture: "clusters", seed: 8675309, sha256: "4f742ab0d3199a66b716e873fdba4c8bb9313d62c0a7a0102d710b7e82faf60f"},
		{fixture: "hierarchy_subgraphs", seed: 1, sha256: "7f7413f41fc82de7ecc00ff9db943b77bb35b48502916a409df65080db4344bb"},
		{fixture: "hierarchy_subgraphs", seed: 123, sha256: "7a70f1af44050630ee7df5975ca5cc3965dfa8389013143da970b2f0187fde49"},
		{fixture: "hierarchy_subgraphs", seed: 8675309, sha256: "3b033ca8cfc0b15d9827fe7728b907cd10feacb348ff5eb64e07437ff2dd9f28"},
		{fixture: "simple_container_hierarchy", seed: 1, sha256: "c0acc7c6967e39fe42d1caca89812e907e316e6067e601af1d9032a3d064714b"},
		{fixture: "simple_container_hierarchy", seed: 123, sha256: "41372d36020e12c44bdf9135fe42cb565dcce5a8cdb69c3ae3c4647a49574121"},
		{fixture: "simple_container_hierarchy", seed: 8675309, sha256: "d7e4ef56bef99c4ae854a5a44d45dcffd93513a09b1b88719a5c5cb261101485"},
		{fixture: "tree-n-seq", seed: 1, sha256: "2e91d8a8758012a6f88110f7033a21fb45a6543f02537757efde484735bfebe0"},
		{fixture: "tree-n-seq", seed: 123, sha256: "23d270f80f044eff9db4cfff2f52c2124b952b29177e3442665e1f9ed5cce14b"},
		{fixture: "tree-n-seq", seed: 8675309, sha256: "68f40e15b4794a5b6704dccf6fd93a9de446780bb2195ada80192919b6ddf0a9"},
	}

	for _, tc := range testCases {
		t.Run(tc.fixture+"/"+strconv.FormatInt(tc.seed, 10), func(t *testing.T) {
			sourcePath := filepath.Join(layoutTestDir, tc.fixture, "graph.input.json")
			input, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			temporaryPath := filepath.Join(t.TempDir(), "graph.input.json")
			if err := os.WriteFile(temporaryPath, input, 0o600); err != nil {
				t.Fatal(err)
			}

			ctx := withTestLogger(t.Context(), t)
			graph, err := readGraph(ctx, temporaryPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := layoutWithSnapshots(ctx, graph, tc.seed, false); err != nil {
				t.Fatal(err)
			}
			serialized, err := graphjson.Serialize(ctx, graph)
			if err != nil {
				t.Fatal(err)
			}
			output, err := json.Marshal(serialized)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(output)
			got := hex.EncodeToString(digest[:])
			if got != tc.sha256 {
				t.Fatalf("layout fingerprint = %s, want %s", got, tc.sha256)
			}
		})
	}
}
