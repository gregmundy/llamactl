// gguf-inspect dumps the top-level metadata of a GGUF file via the
// existing internal/gguf parser. Used as a one-shot diagnostic when a
// model in `llamactl list` shows blank PARAMS.
//
// Usage:
//
//	go run ./cmd/gguf-inspect <path-to-gguf>
package main

import (
	"fmt"
	"os"

	"github.com/gregmundy/llamactl/internal/gguf"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gguf-inspect <file.gguf>")
		os.Exit(2)
	}
	h, err := gguf.ReadHeader(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("GGUF version:    %d\n", h.Version)
	fmt.Printf("Tensor count:    %d\n", h.TensorCount)
	fmt.Printf("Architecture:    %s\n", h.Architecture)
	fmt.Printf("ParamsCount:     %d (raw)\n", h.ParamsCount)
	fmt.Printf("ContextLength:   %d\n", h.ContextLength)
}
