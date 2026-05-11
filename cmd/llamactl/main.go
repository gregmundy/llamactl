package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gregmundy/llamactl/internal/cli"
)

var llamactlVersion = "dev"

func main() {
	deps := &cli.Deps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	root := cli.NewRoot(deps, llamactlVersion)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, cli.ErrUserError) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "llamactl:", err)
		os.Exit(1)
	}
}
