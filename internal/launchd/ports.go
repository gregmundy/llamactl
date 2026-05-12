// Package launchd ports.go — discover ports claimed by sibling
// com.llamactl.* services via their plist files.
package launchd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// llamactlPlistPrefix is the label prefix llamactl uses for the
// LaunchAgent files it writes. PortsInUse and PortFor both filter on
// this so non-llamactl plists in the same dir are ignored.
const llamactlPlistPrefix = "com.llamactl."

// PortsInUse scans dir for com.llamactl.*.plist files and extracts the
// `--port N` arg from each plist's ProgramArguments. Returns the ports
// in arbitrary order. A missing directory is not an error — it returns
// (nil, nil), matching the "fresh install, no services yet" case.
func PortsInUse(dir string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, llamactlPlistPrefix) || !strings.HasSuffix(name, ".plist") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if p := extractPortArg(data); p > 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

// PortFor returns the `--port` arg from the named label's plist in dir,
// or 0 if the plist is missing or doesn't contain a parseable port.
// Useful for "re-serving an existing service" — the caller can exclude
// its current port from the skip list so it keeps using the same port.
func PortFor(dir, label string) int {
	path := filepath.Join(dir, label+".plist")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return extractPortArg(data)
}

// HasAPIKey reports whether the plist for label contains an --api-key arg.
// Missing plist returns false.
func HasAPIKey(dir, label string) bool {
	data, err := os.ReadFile(filepath.Join(dir, label+".plist"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "<string>--api-key</string>")
}

// HasPublicBind reports whether the plist for label binds publicly.
// Missing --host arg is treated as default-public (llama-server binds 0.0.0.0
// by default). Explicit 127.0.0.1 / ::1 / localhost is non-public.
// Missing plist returns false (no service to flag).
func HasPublicBind(dir, label string) bool {
	data, err := os.ReadFile(filepath.Join(dir, label+".plist"))
	if err != nil {
		return false
	}
	s := string(data)
	idx := strings.Index(s, "<string>--host</string>")
	if idx < 0 {
		return true // default-bind is public
	}
	rest := s[idx+len("<string>--host</string>"):]
	open := strings.Index(rest, "<string>")
	if open < 0 {
		return true
	}
	rest = rest[open+len("<string>"):]
	closeIdx := strings.Index(rest, "</string>")
	if closeIdx < 0 {
		return true
	}
	host := strings.TrimSpace(rest[:closeIdx])
	return host != "127.0.0.1" && host != "::1" && host != "localhost"
}

// HasDraft returns the path embedded in the plist's --model-draft arg, if
// any. Returns ("", false) when the plist is missing, the flag is absent,
// or the value <string> can't be parsed. Mirrors the scanning pattern of
// HasAPIKey / HasPublicBind.
func HasDraft(dir, label string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, label+".plist"))
	if err != nil {
		return "", false
	}
	s := string(data)
	idx := strings.Index(s, "<string>--model-draft</string>")
	if idx < 0 {
		return "", false
	}
	rest := s[idx+len("<string>--model-draft</string>"):]
	open := strings.Index(rest, "<string>")
	if open < 0 {
		return "", false
	}
	rest = rest[open+len("<string>"):]
	closeIdx := strings.Index(rest, "</string>")
	if closeIdx < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:closeIdx]), true
}

// extractPortArg finds the value of `--port N` inside the plist's
// ProgramArguments. The plist template emits each arg as a <string>
// element; we scan for `<string>--port</string>` and read the next
// <string>...</string> as the integer value.
func extractPortArg(data []byte) int {
	s := string(data)
	idx := strings.Index(s, "<string>--port</string>")
	if idx < 0 {
		return 0
	}
	rest := s[idx+len("<string>--port</string>"):]
	open := strings.Index(rest, "<string>")
	if open < 0 {
		return 0
	}
	rest = rest[open+len("<string>"):]
	closeIdx := strings.Index(rest, "</string>")
	if closeIdx < 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:closeIdx]))
	if err != nil {
		return 0
	}
	return n
}
