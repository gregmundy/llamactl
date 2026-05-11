package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runRoot executes the root command with the given args using deps for I/O
// and returns captured stdout, stderr, and any error.
func runRoot(t *testing.T, deps *Deps, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	deps.Stdout = &out
	deps.Stderr = &errOut
	root := NewRoot(deps, "test")
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestRoot_VersionFlag(t *testing.T) {
	deps := &Deps{}
	out, _, err := runRoot(t, deps, "--version")
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.Contains(out, "test") {
		t.Fatalf("expected version string in output, got: %q", out)
	}
}

func TestRoot_HelpShowsShortDescription(t *testing.T) {
	deps := &Deps{}
	out, _, err := runRoot(t, deps, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	// Cobra's default --help for a non-runnable, no-subcommand root prints
	// only the Short string. Once subcommands land in later tasks, the full
	// usage block (including "llamactl") will appear.
	if !strings.Contains(out, "Run llama.cpp on Apple Silicon") {
		t.Fatalf("expected Short string in help output, got: %q", out)
	}
}
