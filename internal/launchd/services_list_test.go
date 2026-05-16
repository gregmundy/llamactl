package launchd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const fakePlistFmt = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/llama-server</string>
    <string>--model</string>
    <string>/tmp/foo.gguf</string>
    <string>--port</string>
    <string>%d</string>
    <string>--ctx-size</string>
    <string>8192</string>
    <string>--cache-type-k</string>
    <string>f16</string>
    <string>--cache-type-v</string>
    <string>f16</string>
  </array>
</dict>
</plist>
`

func TestListRunningServices(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(label string, port int) {
		t.Helper()
		body := fmt.Sprintf(fakePlistFmt, label, port)
		if err := os.WriteFile(filepath.Join(dir, label+".plist"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("com.llamactl.qwen2.5-3b-instruct", 8082)
	mustWrite("com.llamactl.gemma-4-e4b-it", 8083)
	mustWrite("com.llamactl.telemetryd", 18080) // must be excluded
	mustWrite("com.apple.something", 9999)      // wrong prefix → excluded

	got, err := ListRunningServices(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2: %+v", len(got), got)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
	if got[0].ID != "gemma-4-e4b-it" || got[0].Port != 8083 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].ID != "qwen2.5-3b-instruct" || got[1].Port != 8082 {
		t.Errorf("got[1] = %+v", got[1])
	}
	// Args must include the recipe-identifying flags.
	if !contains(got[0].Args, "--ctx-size") {
		t.Errorf("got[0].Args missing --ctx-size: %v", got[0].Args)
	}
	if !contains(got[0].Args, "--cache-type-k") {
		t.Errorf("got[0].Args missing --cache-type-k: %v", got[0].Args)
	}
	// Args must NOT contain the binary path.
	for _, a := range got[0].Args {
		if a == "/opt/homebrew/bin/llama-server" {
			t.Errorf("Args should exclude binary path, found: %v", got[0].Args)
		}
	}
}

func TestListRunningServices_MissingDir(t *testing.T) {
	got, err := ListRunningServices(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}
