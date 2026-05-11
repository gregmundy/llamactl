package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecRunner_RunCapturesStdout(t *testing.T) {
	var out bytes.Buffer
	err := ExecRunner{}.Run(context.Background(), "echo", []string{"hello"}, "", &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run echo: %v", err)
	}
	if strings.TrimSpace(out.String()) != "hello" {
		t.Fatalf("got stdout %q, want %q", out.String(), "hello")
	}
}

func TestExecRunner_RunReturnsErrorOnNonZeroExit(t *testing.T) {
	err := ExecRunner{}.Run(context.Background(), "sh", []string{"-c", "exit 7"}, "", &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected non-nil error from exit 7")
	}
}
