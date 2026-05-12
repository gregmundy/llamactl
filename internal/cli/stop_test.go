package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamactl/internal/launchd"
)

func TestStopUnknownModel(t *testing.T) {
	d := &Deps{
		LaunchdService:  &fakeLaunchdService{},
		LaunchAgentsDir: t.TempDir(),
	}
	_, _, err := runRoot(t, d, "stop", "nope")
	if err == nil || !strings.Contains(err.Error(), "no detached service") {
		t.Fatalf("err = %v, want 'no detached service'", err)
	}
}

func TestStopOneRemovesPlist(t *testing.T) {
	tmp := t.TempDir()
	label := "com.llamactl.qwen"
	plistPath := filepath.Join(tmp, label+".plist")
	if err := os.WriteFile(plistPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ld := &fakeLaunchdService{}
	d := &Deps{LaunchdService: ld, LaunchAgentsDir: tmp}
	_, _, err := runRoot(t, d, "stop", "qwen")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ld.Booted) != 1 || ld.Booted[0] != label {
		t.Errorf("Bootout calls = %v, want [%q]", ld.Booted, label)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Errorf("plist should be deleted; got err=%v", err)
	}
}

func TestStopAllIteratesServices(t *testing.T) {
	tmp := t.TempDir()
	for _, label := range []string{
		"com.llamactl.qwen2.5-7b-instruct",
		"com.llamactl.mistral-7b-v0.3",
	} {
		_ = os.WriteFile(filepath.Join(tmp, label+".plist"), []byte("x"), 0o644)
	}
	ld := &fakeLaunchdService{
		ListResult: []launchd.ServiceInfo{
			{Label: "com.llamactl.qwen2.5-7b-instruct", PlistPath: filepath.Join(tmp, "com.llamactl.qwen2.5-7b-instruct.plist")},
			{Label: "com.llamactl.mistral-7b-v0.3", PlistPath: filepath.Join(tmp, "com.llamactl.mistral-7b-v0.3.plist")},
		},
	}
	d := &Deps{LaunchdService: ld, LaunchAgentsDir: tmp}
	_, _, err := runRoot(t, d, "stop")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ld.Booted) != 2 {
		t.Errorf("Bootout calls = %d, want 2", len(ld.Booted))
	}
}

func TestStopAllEmpty(t *testing.T) {
	d := &Deps{LaunchdService: &fakeLaunchdService{}, LaunchAgentsDir: t.TempDir()}
	out, _, err := runRoot(t, d, "stop")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no llamactl services") {
		t.Errorf("out = %q, want 'no llamactl services'", out)
	}
}
