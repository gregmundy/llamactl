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
		func() (string, error) { return "/opt/homebrew/Caskroom/llamactl/1.3.0/llamactl", nil },
		nil)
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "already on latest") {
		t.Fatalf("missing 'already on latest':\n%s", s)
	}
	// Spec §5.1: on-latest output must NOT include current:/latest: lines.
	if strings.Contains(s, "current:") || strings.Contains(s, "latest:") {
		t.Fatalf("on-latest output must not include current:/latest: lines:\n%s", s)
	}
}

func TestUpdateBrewInstalledRunsBrew(t *testing.T) {
	var out bytes.Buffer
	runner := &recordingRunner{}
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	err := runUpdate(context.Background(), d, "v1.2.0", false,
		func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
		func() (string, error) { return "/opt/homebrew/Caskroom/llamactl/1.2.0/llamactl", nil },
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

func TestUpdateBrewSymlinkDetected(t *testing.T) {
	var out bytes.Buffer
	runner := &recordingRunner{}
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	err := runUpdate(context.Background(), d, "v1.2.0", false,
		func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
		func() (string, error) { return "/opt/homebrew/bin/llamactl", nil },
		runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) == 0 {
		t.Fatal("/opt/homebrew/bin/llamactl symlink not detected as brew install; expected brew runner calls")
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
		func() (string, error) { return "/usr/local/Caskroom/llamactl/1.2.0/llamactl", nil },
		runner)
	if len(runner.calls) == 0 {
		t.Fatal("Intel brew Caskroom path not detected; expected brew runner calls")
	}
}

func TestUpdateRefreshBypassesCache(t *testing.T) {
	var gotRefresh bool
	var out bytes.Buffer
	runner := &recordingRunner{}
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	fetcher := func(ctx context.Context, refresh bool) (string, error) {
		gotRefresh = refresh
		return "1.3.0", nil
	}
	err := runUpdate(context.Background(), d, "v1.2.0", true,
		fetcher,
		func() (string, error) { return "/opt/homebrew/Caskroom/llamactl/1.2.0/llamactl", nil },
		runner)
	if err != nil {
		t.Fatal(err)
	}
	if !gotRefresh {
		t.Fatal("expected fetcher to be called with refresh=true, but got refresh=false")
	}
}

func TestUpdateDevBuildShortCircuits(t *testing.T) {
	var out bytes.Buffer
	runner := &recordingRunner{}
	d := &Deps{Stdout: &out, Stderr: io.Discard}
	err := runUpdate(context.Background(), d, "dev", false,
		func(ctx context.Context, refresh bool) (string, error) { return "1.3.0", nil },
		func() (string, error) { return "/Users/greg/go/bin/llamactl", nil },
		runner)
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "dev builds don't auto-update") {
		t.Fatalf("expected dev-build short-circuit message; got:\n%s", s)
	}
	if len(runner.calls) > 0 {
		t.Fatalf("runner should NOT be called for dev build; got calls: %v", runner.calls)
	}
}
