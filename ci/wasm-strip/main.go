// wasm-strip removes the custom sections of a WebAssembly module in place.
//
// TinyGo's -no-debug drops DWARF but keeps the name section, more than a
// megabyte of function names that no engine needs to run the module. Custom
// sections are skipped by copying bytes, so the code is left exactly as built.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: wasm-strip FILE.wasm")
		os.Exit(2)
	}
	in, err := os.ReadFile(os.Args[1])
	if err == nil {
		var out []byte
		if out, err = strip(in); err == nil {
			err = os.WriteFile(os.Args[1], out, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "wasm-strip:", err)
		os.Exit(1)
	}
}

func strip(in []byte) ([]byte, error) {
	const header = 8 // magic and version
	if len(in) < header || string(in[:4]) != "\x00asm" {
		return nil, fmt.Errorf("not a WebAssembly module")
	}
	out := append(make([]byte, 0, len(in)), in[:header]...)
	for rest := in[header:]; len(rest) > 0; {
		size, n := binary.Uvarint(rest[1:])
		end := 1 + n + int(size)
		if n <= 0 || end > len(rest) {
			return nil, fmt.Errorf("malformed section")
		}
		if rest[0] != 0 { // 0 is the custom section ID
			out = append(out, rest[:end]...)
		}
		rest = rest[end:]
	}
	return out, nil
}
