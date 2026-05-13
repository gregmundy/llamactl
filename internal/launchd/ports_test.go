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

func TestHasAPIKey(t *testing.T) {
	dir := t.TempDir()
	withKey := `<plist><array><string>--api-key</string><string>sk-XYZ</string></array></plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.with.plist"), []byte(withKey), 0o644); err != nil {
		t.Fatal(err)
	}
	withoutKey := `<plist><array><string>--port</string><string>8080</string></array></plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.without.plist"), []byte(withoutKey), 0o644); err != nil {
		t.Fatal(err)
	}

	if !HasAPIKey(dir, "com.llamactl.with") {
		t.Fatal("expected true for plist containing --api-key")
	}
	if HasAPIKey(dir, "com.llamactl.without") {
		t.Fatal("expected false for plist without --api-key")
	}
	if HasAPIKey(dir, "com.llamactl.missing") {
		t.Fatal("expected false for missing plist")
	}
}

func TestHasPublicBind(t *testing.T) {
	dir := t.TempDir()
	defaultBind := `<plist><array><string>--port</string><string>8080</string></array></plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.default.plist"), []byte(defaultBind), 0o644); err != nil {
		t.Fatal(err)
	}
	publicBind := `<plist><array><string>--host</string><string>0.0.0.0</string></array></plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.public.plist"), []byte(publicBind), 0o644); err != nil {
		t.Fatal(err)
	}
	loopback := `<plist><array><string>--host</string><string>127.0.0.1</string></array></plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.loopback.plist"), []byte(loopback), 0o644); err != nil {
		t.Fatal(err)
	}
	ipv6Loopback := `<plist><array><string>--host</string><string>::1</string></array></plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.llamactl.ipv6.plist"), []byte(ipv6Loopback), 0o644); err != nil {
		t.Fatal(err)
	}

	if !HasPublicBind(dir, "com.llamactl.default") {
		t.Fatal("missing --host defaults public")
	}
	if !HasPublicBind(dir, "com.llamactl.public") {
		t.Fatal("explicit 0.0.0.0 is public")
	}
	if HasPublicBind(dir, "com.llamactl.loopback") {
		t.Fatal("127.0.0.1 is not public")
	}
	if HasPublicBind(dir, "com.llamactl.ipv6") {
		t.Fatal("::1 is not public")
	}
	if HasPublicBind(dir, "com.llamactl.missing") {
		t.Fatal("missing plist returns false (no service to flag)")
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

func TestHasDraftFindsEmbeddedPath(t *testing.T) {
	dir := t.TempDir()
	label := "com.llamactl.qwen2.5-32b-instruct"
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/llama-server</string>
    <string>--model</string>
    <string>/Users/greg/.local/share/llama-models/qwen2.5-32b-instruct/Q4_K_M.gguf</string>
    <string>--model-draft</string>
    <string>/Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q4_K_M.gguf</string>
    <string>--ctx-size-draft</string>
    <string>8192</string>
  </array>
</dict>
</plist>`
	if err := os.WriteFile(filepath.Join(dir, label+".plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	path, ok := HasDraft(dir, label)
	if !ok {
		t.Fatalf("expected HasDraft to return ok=true")
	}
	want := "/Users/greg/.local/share/llama-models/qwen2.5-3b-instruct/Q4_K_M.gguf"
	if path != want {
		t.Errorf("HasDraft path = %q, want %q", path, want)
	}
}

func TestHasDraftAbsent(t *testing.T) {
	dir := t.TempDir()
	label := "com.llamactl.no-draft"
	plist := `<plist><dict><key>ProgramArguments</key><array>
    <string>/usr/local/bin/llama-server</string>
    <string>--model</string>
    <string>/path/main.gguf</string>
    </array></dict></plist>`
	if err := os.WriteFile(filepath.Join(dir, label+".plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	path, ok := HasDraft(dir, label)
	if ok || path != "" {
		t.Errorf("HasDraft = (%q, %v), want (\"\", false)", path, ok)
	}
}

func TestHasDraftMissingPlist(t *testing.T) {
	path, ok := HasDraft(t.TempDir(), "com.llamactl.does-not-exist")
	if ok || path != "" {
		t.Errorf("HasDraft on missing plist = (%q, %v), want (\"\", false)", path, ok)
	}
}
