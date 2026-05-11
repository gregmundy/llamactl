// Package server resolves and probes the llama-server binary. The resolver
// follows PRD §4 discovery order; the probe runs `llama-server --version`
// once and caches the parsed output. Both are constructed in main.go and
// passed to cli via narrow interfaces.
package server

import (
	"fmt"
	"regexp"
	"strconv"
)

// Version is the parsed output of `llama-server --version`.
type Version struct {
	Build int    // monotonically increasing release counter
	SHA   string // upstream git short SHA
	Raw   string // first line of output, verbatim — useful for printing
}

func (v Version) String() string {
	if v.Build == 0 && v.SHA == "" {
		return v.Raw
	}
	return fmt.Sprintf("b%d (%s)", v.Build, v.SHA)
}

// AtLeast reports whether this version's Build is >= minBuild. Used by
// recipe assembly to gate flags like --flash-attn that require a minimum
// llama.cpp version.
func (v Version) AtLeast(minBuild int) bool { return v.Build >= minBuild }

// versionLineRe matches "version: 4567 (a1b2c3d4)" with an optional "b" prefix.
var versionLineRe = regexp.MustCompile(`version:\s+b?(\d+)\s+\(([^)]+)\)`)

// ParseVersion extracts the build number and SHA from `llama-server --version`
// stdout. Only the first matching line is used.
func ParseVersion(s string) (Version, error) {
	m := versionLineRe.FindStringSubmatch(s)
	if len(m) != 3 {
		return Version{}, fmt.Errorf("unrecognized llama-server --version output: %q", s)
	}
	build, err := strconv.Atoi(m[1])
	if err != nil {
		return Version{}, fmt.Errorf("parse build %q: %w", m[1], err)
	}
	return Version{Build: build, SHA: m[2], Raw: s}, nil
}
