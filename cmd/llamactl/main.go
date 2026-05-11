package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregmundy/llamactl/internal/cli"
	"github.com/gregmundy/llamactl/internal/config"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/runner"
)

var llamactlVersion = "dev"

func main() {
	paths, err := config.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamactl: cannot resolve home directory:", err)
		os.Exit(1)
	}
	run := runner.ExecRunner{}

	deps := &cli.Deps{
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		HardwareDetector: &hardware.Detector{Runner: run},
		HardwareJSONPath: paths.HardwareJSON(),
		Now:              time.Now,
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
