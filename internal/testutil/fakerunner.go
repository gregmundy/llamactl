// Package testutil holds test-only utilities shared across llamactl packages.
package testutil

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// FakeRunner is a controllable runner.CommandRunner used in tests.
//
// Keys map by "name + ' ' + strings.Join(args, ' ')" (or just "name" when args
// is empty). Set Outputs to inject stdout strings; set Errs to inject non-nil
// errors. Every call is recorded in Calls in the order it happened.
//
// Unknown keys are a no-op (returns nil with no writes), matching the silent
// pass-through behavior the migrated launchd/proc fakes already relied on. The
// resolver and hardware tests fully populate fixtures, so they never depend on
// the unknown-call branch.
type FakeRunner struct {
	Outputs map[string]string
	Errs    map[string]error
	Calls   []string
}

// Run satisfies runner.CommandRunner.
func (f *FakeRunner) Run(_ context.Context, name string, args []string, _ string,
	stdout, _ io.Writer) error {
	key := name
	if len(args) > 0 {
		key = name + " " + strings.Join(args, " ")
	}
	f.Calls = append(f.Calls, key)
	if err, ok := f.Errs[key]; ok {
		return err
	}
	if out, ok := f.Outputs[key]; ok && stdout != nil {
		fmt.Fprint(stdout, out)
	}
	return nil
}
