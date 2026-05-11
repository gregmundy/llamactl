// Package hardware introspects an Apple Silicon Mac's chip, memory, GPU
// memory cap, and hypervisor state. Every probe is best-effort: a failing
// sysctl or system_profiler invocation leaves the corresponding Info field
// at its zero value rather than failing the whole detection. Doctor is the
// component that converts zero values into actionable error messages.
package hardware

import (
	"context"
	"io"
)

// CommandRunner mirrors runner.CommandRunner — we redeclare locally to avoid
// importing the runner package from a leaf detection package. The cli wiring
// passes the same concrete ExecRunner to both.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Detector probes host hardware via CommandRunner. Construct with the
// production runner.ExecRunner in main.go, or a fake in tests.
type Detector struct {
	Runner CommandRunner
}

// Detect runs every probe and returns an Info populated with whatever
// succeeded. The error return is reserved for future use (e.g. catastrophic
// runner failure); today Detect never returns a non-nil error.
func (d *Detector) Detect(ctx context.Context) (Info, error) {
	var info Info
	// Each probe is filled in by later tasks (7–10). The skeleton just
	// ensures the public surface is stable.
	return info, nil
}
