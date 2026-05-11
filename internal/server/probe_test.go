package server

import (
	"context"
	"errors"
	"io"
	"testing"
)

// countingRunner counts how many times Run is called and returns canned
// stdout. Used to verify caching behavior in the prober.
type countingRunner struct {
	calls int
	out   string
	err   error
}

func (c *countingRunner) Run(_ context.Context, _ string, _ []string, _ string, stdout, _ io.Writer) error {
	c.calls++
	if c.err != nil {
		return c.err
	}
	_, _ = io.WriteString(stdout, c.out)
	return nil
}

func TestProbe_RunsOnceAndCaches(t *testing.T) {
	r := &countingRunner{out: "version: 4567 (a1b2c3d4)\n"}
	p := &Prober{Runner: r}

	v1, err := p.Probe(context.Background(), "/bin/llama-server")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := p.Probe(context.Background(), "/bin/llama-server")
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 {
		t.Fatalf("cache mismatch: %v vs %v", v1, v2)
	}
	if r.calls != 1 {
		t.Fatalf("expected one runner call, got %d", r.calls)
	}
}

func TestProbe_BinaryError(t *testing.T) {
	p := &Prober{Runner: &countingRunner{err: errors.New("exec failed")}}
	if _, err := p.Probe(context.Background(), "/bin/llama-server"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProbe_PathChangeInvalidatesCache(t *testing.T) {
	r := &countingRunner{out: "version: 4567 (a1b2c3d4)\n"}
	p := &Prober{Runner: r}
	if _, err := p.Probe(context.Background(), "/path/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Probe(context.Background(), "/path/b"); err != nil {
		t.Fatal(err)
	}
	if r.calls != 2 {
		t.Fatalf("expected two runner calls after path change, got %d", r.calls)
	}
}
