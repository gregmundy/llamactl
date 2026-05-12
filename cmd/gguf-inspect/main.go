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
	h0, err := gguf.ReadHeader(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	h, err := gguf.ReadHeaderWithTensors(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("GGUF version:    %d\n", h.Version)
	fmt.Printf("Tensor count:    %d\n", h.TensorCount)
	fmt.Printf("Architecture:    %s\n", h.Architecture)

	// Branch on whether the kv-block produced 0 but the walk recovered a value
	if h0.ParamsCount == 0 && h.ParamsCount != 0 {
		fmt.Printf("ParamsCount:     %d (via tensor-shape fallback)\n", h.ParamsCount)
	} else {
		fmt.Printf("ParamsCount:     %d\n", h.ParamsCount)
	}

	fmt.Printf("ContextLength:   %d\n", h.ContextLength)
}
