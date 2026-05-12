package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
)

func TestStatusEmpty(t *testing.T) {
	d := &Deps{
		LaunchdService:  &fakeLaunchdService{ListResult: nil},
		LaunchAgentsDir: t.TempDir(),
	}
	out, _, err := runRoot(t, d, "status")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "no detached services") {
		t.Errorf("out = %q, want 'no detached services'", out)
	}
}

func writeMinimalPlist(t *testing.T, path string, port int) {
	t.Helper()
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/llama-server</string>
    <string>--port</string>
    <string>%d</string>
  </array>
</dict>
</plist>`, port)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatusRunningService(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.qwen2.5-7b-instruct.plist")
	writeMinimalPlist(t, plistPath, 8080)

	logsDir := filepath.Join(tmp, "logs")
	d := &Deps{
		LaunchdService: &fakeLaunchdService{
			ListResult: []launchd.ServiceInfo{
				{Label: "com.llamactl.qwen2.5-7b-instruct", PlistPath: plistPath, PID: 12345, State: "running"},
			},
			Services: map[string]launchd.ServiceInfo{
				"com.llamactl.qwen2.5-7b-instruct": {Label: "com.llamactl.qwen2.5-7b-instruct", PID: 12345, State: "running"},
			},
		},
		ProcInspector: &fakeProcInspector{
			RSSByPID:    map[int]int64{12345: 4_000_000_000},
			UptimeByPID: map[int]time.Duration{12345: 3725 * time.Second},
		},
		TokRateReader:   &fakeTokRateReader{RateByPath: map[string]float64{filepath.Join(logsDir, "qwen2.5-7b-instruct.log"): 123.4}},
		LaunchAgentsDir: tmp,
		LogsDir:         logsDir,
	}
	out, _, err := runRoot(t, d, "status")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "qwen2.5-7b-instruct") {
		t.Errorf("out missing model id:\n%s", out)
	}
	if !strings.Contains(out, "8080") {
		t.Errorf("out missing port:\n%s", out)
	}
	if !strings.Contains(out, "123.4") {
		t.Errorf("out missing tok/s:\n%s", out)
	}
}

func TestStatusStoppedService(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.qwen.plist")
	writeMinimalPlist(t, plistPath, 8080)
	d := &Deps{
		LaunchdService: &fakeLaunchdService{
			ListResult: []launchd.ServiceInfo{
				{Label: "com.llamactl.qwen", PlistPath: plistPath, PID: 0, State: ""},
			},
		},
		LaunchAgentsDir: tmp,
	}
	out, _, err := runRoot(t, d, "status")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("out should show 'stopped':\n%s", out)
	}
}

func TestStatusJSONFormat(t *testing.T) {
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.llamactl.qwen.plist")
	writeMinimalPlist(t, plistPath, 8080)
	d := &Deps{
		LaunchdService: &fakeLaunchdService{
			ListResult: []launchd.ServiceInfo{
				{Label: "com.llamactl.qwen", PlistPath: plistPath, PID: 555, State: "running"},
			},
			Services: map[string]launchd.ServiceInfo{
				"com.llamactl.qwen": {Label: "com.llamactl.qwen", PID: 555, State: "running"},
			},
		},
		ProcInspector:   &fakeProcInspector{RSSByPID: map[int]int64{555: 1024}, UptimeByPID: map[int]time.Duration{555: time.Second}},
		TokRateReader:   &fakeTokRateReader{},
		LaunchAgentsDir: tmp,
		LogsDir:         tmp,
	}
	out, _, err := runRoot(t, d, "status", "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, `"model_id"`) || !strings.Contains(out, `"qwen"`) {
		t.Errorf("JSON out missing fields:\n%s", out)
	}
	if !strings.Contains(out, `"pid": 555`) {
		t.Errorf("JSON should contain pid:\n%s", out)
	}
}
