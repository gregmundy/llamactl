package testutil

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestFakeRunnerOutputs(t *testing.T) {
	f := &FakeRunner{Outputs: map[string]string{"sysctl -n hw.ncpu": "8\n"}}
	var buf bytes.Buffer
	if err := f.Run(context.Background(), "sysctl", []string{"-n", "hw.ncpu"}, "", &buf, nil); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "8\n" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestFakeRunnerErrs(t *testing.T) {
	sentinel := errors.New("boom")
	f := &FakeRunner{Errs: map[string]error{"foo bar": sentinel}}
	if err := f.Run(context.Background(), "foo", []string{"bar"}, "", nil, nil); !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}
}

func TestFakeRunnerRecordsCalls(t *testing.T) {
	f := &FakeRunner{}
	_ = f.Run(context.Background(), "a", []string{"b"}, "", nil, nil)
	_ = f.Run(context.Background(), "c", nil, "", nil, nil)
	if len(f.Calls) != 2 || f.Calls[0] != "a b" || f.Calls[1] != "c" {
		t.Fatalf("calls=%v", f.Calls)
	}
}

func TestFakeRunnerUnknownIsNoop(t *testing.T) {
	f := &FakeRunner{}
	var buf bytes.Buffer
	if err := f.Run(context.Background(), "anything", []string{"--flag"}, "", &buf, nil); err != nil {
		t.Fatalf("got err %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote unexpected bytes: %q", buf.String())
	}
	if len(f.Calls) != 1 || f.Calls[0] != "anything --flag" {
		t.Fatalf("calls=%v", f.Calls)
	}
}
