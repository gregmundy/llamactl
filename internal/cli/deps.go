// Package cli builds the cobra command tree and orchestrates llamactl flows.
// The package never imports its concrete dependencies — instead it consumes
// the narrow interfaces below, which the binary wires up at main.go.
package cli

import (
	"errors"
	"io"
)

// ErrUserError marks errors caused by user input or environment state
// (no llama-server found, VM detected, etc.). main.go maps this to exit 2.
var ErrUserError = errors.New("user error")

// Deps collects everything the cli subcommands need. Later tasks add fields.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer
}
