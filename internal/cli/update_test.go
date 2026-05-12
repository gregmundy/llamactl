package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// recordingRunner satisfies runner.CommandRunner and records calls.
type recordingRunner struct {
	calls []string
	err   error
}

func (r *recordingRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return r.err
}

func TestUpdateOnLatest(t *testing.T) {
	var out bytes.Buffer
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	err := runUpdate(context.Background(), d, "v1.3.0", false,
		func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
		func() (string, error) { return "/opt/homebrew/Cellar/llamactl/1.3.0/bin/llamactl", nil },
		nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already on latest") {
		t.Fatalf("missing 'already on latest':\n%s", out.String())
	}
}

func TestUpdateBrewInstalledRunsBrew(t *testing.T) {
	var out bytes.Buffer
	runner := &recordingRunner{}
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	err := runUpdate(context.Background(), d, "v1.2.0", false,
		func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
		func() (string, error) { return "/opt/homebrew/Cellar/llamactl/1.2.0/bin/llamactl", nil },
		runner)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range runner.calls {
		if strings.Contains(c, "brew upgrade gregmundy/tap/llamactl") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected brew upgrade call; captured: %v", runner.calls)
	}
}

func TestUpdateNonBrewInstallMessage(t *testing.T) {
	var out bytes.Buffer
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	err := runUpdate(context.Background(), d, "v1.2.0", false,
		func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
		func() (string, error) { return "/Users/greg/go/bin/llamactl", nil },
		nil)
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "not installed via Homebrew") {
		t.Fatalf("missing non-brew message:\n%s", s)
	}
}

func TestUpdateIntelBrewPathDetected(t *testing.T) {
	var out bytes.Buffer
	runner := &recordingRunner{}
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	_ = runUpdate(context.Background(), d, "v1.2.0", false,
		func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
		func() (string, error) { return "/usr/local/Cellar/llamactl/1.2.0/bin/llamactl", nil },
		runner)
	if len(runner.calls) == 0 {
		t.Fatal("Intel brew path not detected; expected brew runner calls")
	}
}
