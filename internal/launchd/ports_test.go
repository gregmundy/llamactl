package launchd

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestPortsInUse(t *testing.T) {
	dir := t.TempDir()
	plist := `<?xml version="1.0"?>
<plist version="1.0">
<dict>
  <key>Label</key><string>com.llamactl.foo</string>
  <key>ProgramArguments</key>
  <array>
    <string>/path/to/llama-server</string>
    <string>--port</string>
    <string>8082</string>
    <string>--model</string>
    <string>/some/file.gguf</string>
  </array>
</dict>
</plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.foo.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.bar.plist"),
		[]byte(`<plist><array><string>--port</string><string>8083</string></array></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-llamactl plist should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "com.other.service.plist"),
		[]byte(`<plist><array><string>--port</string><string>9999</string></array></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	ports, err := PortsInUse(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Ints(ports)
	want := []int{8082, 8083}
	if len(ports) != len(want) || ports[0] != want[0] || ports[1] != want[1] {
		t.Fatalf("got %v, want %v", ports, want)
	}
}

func TestPortsInUseMissingDir(t *testing.T) {
	ports, err := PortsInUse(filepath.Join(os.TempDir(), "llamactl-definitely-does-not-exist-xyz"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(ports) != 0 {
		t.Fatalf("got %d ports, want 0", len(ports))
	}
}

func TestPortsInUseEmptyDir(t *testing.T) {
	ports, err := PortsInUse(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 0 {
		t.Fatalf("got %v, want []", ports)
	}
}

func TestPortsInUseIgnoresUnparseable(t *testing.T) {
	dir := t.TempDir()
	// A llamactl plist with no --port arg — should be ignored, not error.
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.broken.plist"),
		[]byte(`<plist><array><string>--something-else</string></array></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.ok.plist"),
		[]byte(`<plist><array><string>--port</string><string>8090</string></array></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	ports, err := PortsInUse(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0] != 8090 {
		t.Fatalf("got %v, want [8090]", ports)
	}
}

func TestPortFor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.qwen.plist"),
		[]byte(`<plist><array><string>--port</string><string>8082</string></array></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PortFor(dir, "com.llamactl.qwen"); got != 8082 {
		t.Errorf("PortFor existing: got %d, want 8082", got)
	}
	if got := PortFor(dir, "com.llamactl.missing"); got != 0 {
		t.Errorf("PortFor missing: got %d, want 0", got)
	}
}
