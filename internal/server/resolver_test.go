package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregmundy/llamactl/internal/testutil"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_EnvVarWins(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "from-env", "llama-server")
	touch(t, envPath)
	r := Resolver{
		Getenv: func(k string) string {
			if k == "LLAMACTL_LLAMA_SERVER_PATH" {
				return envPath
			}
			return ""
		},
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    tmp,
		ConfigPath: "/does/not/exist/config.yaml",
		Runner:     &testutil.FakeRunner{Errs: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Path != envPath {
		t.Errorf("Path = %q, want %q", res.Path, envPath)
	}
	if res.Source != SourceEnv {
		t.Errorf("Source = %v, want SourceEnv", res.Source)
	}
}

func TestResolve_ConfigPathSecond(t *testing.T) {
	tmp := t.TempDir()
	cfgServer := filepath.Join(tmp, "from-cfg", "llama-server")
	touch(t, cfgServer)
	cfgFile := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("llama_server_path: "+cfgServer+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    tmp,
		ConfigPath: cfgFile,
		Runner:     &testutil.FakeRunner{Errs: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != cfgServer || res.Source != SourceConfig {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

func TestResolve_PATHThird(t *testing.T) {
	r := Resolver{
		Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "llama-server" {
				return "/usr/local/bin/llama-server", nil
			}
			return "", errors.New("nope")
		},
		HomeDir:    "/no/such/home",
		ConfigPath: "/no/such/config",
		Runner:     &testutil.FakeRunner{Errs: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "/usr/local/bin/llama-server" || res.Source != SourcePATH {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

func TestResolve_LlamavmShimFourth(t *testing.T) {
	tmp := t.TempDir()
	shim := filepath.Join(tmp, ".llamavm", "shims", "llama-server")
	touch(t, shim)
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    tmp,
		ConfigPath: "/no/such",
		Runner:     &testutil.FakeRunner{Errs: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != shim || res.Source != SourceLlamavmShim {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

func TestResolve_BrewFifth(t *testing.T) {
	tmp := t.TempDir()
	brewPrefix := filepath.Join(tmp, "homebrew", "opt", "llama.cpp")
	brewBin := filepath.Join(brewPrefix, "bin", "llama-server")
	touch(t, brewBin)
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    "/no/such",
		ConfigPath: "/no/such",
		Runner: &testutil.FakeRunner{Outputs: map[string]string{
			"brew --prefix llama.cpp": brewPrefix + "\n",
		}},
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != brewBin || res.Source != SourceBrew {
		t.Errorf("got Path=%q Source=%v", res.Path, res.Source)
	}
}

// Resolve must memoize successful results so back-to-back doctor checks
// (resolvable + version floor) don't repeat the LookPath/Stat work.
func TestResolverMemoizes(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "from-env", "llama-server")
	touch(t, envPath)

	calls := 0
	r := &Resolver{
		Getenv: func(k string) string {
			if k == "LLAMACTL_LLAMA_SERVER_PATH" {
				calls++
				return envPath
			}
			return ""
		},
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    tmp,
		ConfigPath: "/does/not/exist/config.yaml",
		Runner:     &testutil.FakeRunner{Errs: map[string]error{"brew --prefix llama.cpp": errors.New("nope")}},
	}
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Getenv (LLAMACTL_LLAMA_SERVER_PATH) called %d times, want 1 (memoization missed)", calls)
	}
}

// Failed Resolves stay re-tryable — only successes are cached.
func TestResolverDoesNotCacheFailures(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "from-env", "llama-server")

	lookups := 0
	r := &Resolver{
		Getenv: func(string) string { return "" },
		LookPath: func(string) (string, error) {
			lookups++
			return "", errors.New("nope")
		},
		HomeDir:    "/no/such",
		ConfigPath: "/no/such",
		Runner:     &testutil.FakeRunner{Errs: map[string]error{"brew --prefix llama.cpp": errors.New("not installed")}},
	}
	if _, err := r.Resolve(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("first: want ErrNotFound, got %v", err)
	}
	// Now make it succeed.
	touch(t, envPath)
	r.Getenv = func(k string) string {
		if k == "LLAMACTL_LLAMA_SERVER_PATH" {
			return envPath
		}
		return ""
	}
	res, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.Path != envPath {
		t.Errorf("Path = %q, want %q (failure was cached!)", res.Path, envPath)
	}
	if lookups < 1 {
		t.Errorf("LookPath calls = %d; expected at least 1 on the first (failing) Resolve", lookups)
	}
}

func TestResolve_NoneReturnsErrNotFound(t *testing.T) {
	r := Resolver{
		Getenv:     func(string) string { return "" },
		LookPath:   func(string) (string, error) { return "", errors.New("nope") },
		HomeDir:    "/no/such",
		ConfigPath: "/no/such",
		Runner:     &testutil.FakeRunner{Errs: map[string]error{"brew --prefix llama.cpp": errors.New("not installed")}},
	}
	_, err := r.Resolve(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
