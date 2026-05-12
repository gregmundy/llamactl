package launchd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderMatchesGolden(t *testing.T) {
	spec := PlistSpec{
		Label:       "com.llamactl.qwen2.5-7b-instruct",
		LlamaServer: "/opt/homebrew/bin/llama-server",
		Args: []string{
			"--model", "/Users/greg/.local/share/llama-models/qwen2.5-7b-instruct/Q4_K_M.gguf",
			"--host", "0.0.0.0",
			"--port", "8080",
		},
		LogPath:    "/Users/greg/Library/Logs/llamactl/qwen2.5-7b-instruct.log",
		WorkingDir: "/Users/greg",
	}
	got, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "sample.plist"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Render output != golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderEscapesXMLInArgs(t *testing.T) {
	spec := PlistSpec{
		Label:       "com.llamactl.test",
		LlamaServer: "/x/llama-server",
		Args:        []string{"--model", "/p&t<file>.gguf"},
		LogPath:     "/log",
		WorkingDir:  "/home",
	}
	got, err := Render(spec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if bytes.Contains(got, []byte("/p&t<file>")) {
		t.Errorf("unescaped XML in output: %s", got)
	}
	if !bytes.Contains(got, []byte("/p&amp;t&lt;file&gt;")) {
		t.Errorf("expected escaped chars; got: %s", got)
	}
}
