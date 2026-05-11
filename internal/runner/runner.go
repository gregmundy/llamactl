// Package runner provides an os/exec seam so command execution can be faked
// in tests. All llamactl subcommands that shell out (sysctl, system_profiler,
// llama-server --version, etc.) go through CommandRunner.
package runner

import (
	"context"
	"io"
	"os/exec"
)

// CommandRunner runs an external command. Implementations decide whether to
// shell out for real or simulate.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// ExecRunner is the production CommandRunner backed by os/exec.
type ExecRunner struct{}

// Run invokes name with args in dir (empty dir = cwd), routing stdout/stderr
// to the supplied writers. Returns the underlying os/exec error verbatim so
// callers can use errors.As to inspect *exec.ExitError.
func (ExecRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
