package launchd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gregmundy/llamactl/internal/testutil"
)

func TestServiceLoadInvokesBootstrap(t *testing.T) {
	r := &testutil.FakeRunner{}
	s := &Service{Runner: r, UID: 501}
	if err := s.Load(context.Background(), "/tmp/foo.plist"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "launchctl bootstrap gui/501 /tmp/foo.plist"
	if len(r.Calls) != 1 || r.Calls[0] != want {
		t.Errorf("calls = %v, want [%q]", r.Calls, want)
	}
}

func TestServiceBootoutInvokesBootout(t *testing.T) {
	r := &testutil.FakeRunner{}
	s := &Service{Runner: r, UID: 501}
	if err := s.Bootout(context.Background(), "com.llamactl.foo"); err != nil {
		t.Fatalf("Bootout: %v", err)
	}
	want := "launchctl bootout gui/501/com.llamactl.foo"
	if len(r.Calls) != 1 || r.Calls[0] != want {
		t.Errorf("calls = %v, want [%q]", r.Calls, want)
	}
}

func TestServicePrintParsesLaunchctlOutput(t *testing.T) {
	output := `com.llamactl.qwen = {
	state = running
	pid = 12345
	last exit code = 0
	domain = gui/501
}
`
	r := &testutil.FakeRunner{
		Outputs: map[string]string{
			"launchctl print gui/501/com.llamactl.qwen": output,
		},
	}
	s := &Service{Runner: r, UID: 501}
	info, err := s.Print(context.Background(), "com.llamactl.qwen")
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if info.PID != 12345 {
		t.Errorf("PID = %d, want 12345", info.PID)
	}
	if info.State != "running" {
		t.Errorf("State = %q, want running", info.State)
	}
	if info.LastExit != 0 {
		t.Errorf("LastExit = %d, want 0", info.LastExit)
	}
	if info.Label != "com.llamactl.qwen" {
		t.Errorf("Label = %q", info.Label)
	}
}

func TestServicePrintReturnsZeroPIDOnNonZeroExit(t *testing.T) {
	r := &testutil.FakeRunner{
		Errs: map[string]error{
			"launchctl print gui/501/com.llamactl.nope": errors.New("service does not exist"),
		},
	}
	s := &Service{Runner: r, UID: 501}
	info, err := s.Print(context.Background(), "com.llamactl.nope")
	if err != nil {
		t.Fatalf("Print should NOT return error for unloaded service: %v", err)
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 for unloaded service", info.PID)
	}
	if info.Label != "com.llamactl.nope" {
		t.Errorf("Label = %q", info.Label)
	}
}

func TestServicePrintPartialOutput(t *testing.T) {
	// Service is loaded but spawning; no PID yet.
	output := `com.llamactl.qwen = {
	state = spawning
	domain = gui/501
}
`
	r := &testutil.FakeRunner{
		Outputs: map[string]string{
			"launchctl print gui/501/com.llamactl.qwen": output,
		},
	}
	s := &Service{Runner: r, UID: 501}
	info, err := s.Print(context.Background(), "com.llamactl.qwen")
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0", info.PID)
	}
	if info.State != "spawning" {
		t.Errorf("State = %q, want spawning", info.State)
	}
}

// reference imports so the file compiles
var _ = bytes.NewBuffer
