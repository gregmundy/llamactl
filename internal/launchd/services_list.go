package launchd

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

// RunningService is one llamactl-managed model service entry. The ID is
// the suffix of the plist Label (i.e. the run-name from `serve --detach`).
// Args is the full ProgramArguments slice EXCLUDING the binary path at
// index 0, so callers can pattern-match recipe-identifying flags without
// special-casing index 0.
type RunningService struct {
	ID   string
	Port int
	Args []string
}

// ListRunningServices scans dir for com.llamactl.*.plist files (excluding
// com.llamactl.telemetryd.plist) and returns one entry per plist. A
// missing directory is not an error — returns (nil, nil) matching the
// "fresh install, no services" case used by PortsInUse.
func ListRunningServices(dir string) ([]RunningService, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []RunningService
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, llamactlPlistPrefix) || !strings.HasSuffix(name, ".plist") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, llamactlPlistPrefix), ".plist")
		if id == "telemetryd" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue // best-effort; one unreadable plist must not nuke the rest
		}
		port := extractPortArg(data)
		if port == 0 {
			continue
		}
		args := extractProgramArguments(data)
		out = append(out, RunningService{ID: id, Port: port, Args: args})
	}
	return out, nil
}

// programArgsDoc is the minimal subset of a plist we parse with
// encoding/xml. The plist <dict> interleaves <key>/value pairs; we
// rely on ProgramArguments being the only <array> in our plists.
type programArgsDoc struct {
	Dict struct {
		Keys   []string `xml:"key"`
		Arrays []struct {
			Strings []string `xml:"string"`
		} `xml:"array"`
	} `xml:"dict"`
}

// extractProgramArguments returns all <string> values in the
// ProgramArguments <array> except the first (the binary path).
// Returns nil on parse failure or absence.
func extractProgramArguments(data []byte) []string {
	var doc programArgsDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	for _, k := range doc.Dict.Keys {
		if k == "ProgramArguments" && len(doc.Dict.Arrays) > 0 {
			args := doc.Dict.Arrays[0].Strings
			if len(args) <= 1 {
				return nil
			}
			return args[1:]
		}
	}
	return nil
}
