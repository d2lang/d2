// wasm-size checks the D2.js WebAssembly module against ci/wasm-size-budget.txt:
// its size in bytes, and its size once compressed with Brotli at quality 11,
// which is close to what a browser downloads. ci/build-wasm.sh runs it on every
// module it builds, so a change that brings back code or data this module had
// shed fails the build instead of passing unnoticed.
//
// Usage: go run ./ci/wasm-size MODULE.wasm [BUDGET.txt]
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/d2lang/brotli"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fail("usage: wasm-size MODULE.wasm [BUDGET.txt]")
	}
	budgetPath := "ci/wasm-size-budget.txt"
	if len(os.Args) == 3 {
		budgetPath = os.Args[2]
	}
	budget, err := readBudget(budgetPath)
	if err != nil {
		fail(err.Error())
	}
	module, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err.Error())
	}
	var compressed bytes.Buffer
	w := brotli.NewWriterOptions(&compressed, brotli.WriterOptions{Quality: 11, LGWin: 24})
	if _, err := w.Write(module); err != nil {
		fail(err.Error())
	}
	if err := w.Close(); err != nil {
		fail(err.Error())
	}

	sizes := map[string]int{"raw": len(module), "brotli": compressed.Len()}
	over := false
	for _, kind := range []string{"raw", "brotli"} {
		size, limit := sizes[kind], budget[kind]
		fmt.Printf("%s: %d bytes, budget %d (%+.1f%%)\n", kind, size, limit, 100*float64(size-limit)/float64(limit))
		over = over || size > limit
	}
	if over {
		fail("the module is over its size budget in " + budgetPath + ". Find what grew it before raising the budget: " +
			"`tinygo build -size full` without -no-debug shows what each package adds.")
	}
}

// readBudget reads lines of the form "raw 10900000" and "brotli 3700000";
// blank lines and lines starting with # are ignored.
func readBudget(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	budget := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s: expected \"KIND BYTES\", got %q", path, line)
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		budget[fields[0]] = n
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for _, kind := range []string{"raw", "brotli"} {
		if budget[kind] <= 0 {
			return nil, fmt.Errorf("%s: no %s budget", path, kind)
		}
	}
	return budget, nil
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "wasm-size:", msg)
	os.Exit(1)
}
