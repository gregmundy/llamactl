package launchd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CommandRunner is the subprocess seam, locally redeclared so this
// package doesn't import internal/runner (same Go structural-typing
// pattern used in internal/hardware and internal/server).
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin string, stdout, stderr io.Writer) error
}

// Service issues launchctl subcommands in the gui/<UID> domain.
type Service struct {
	Runner CommandRunner
	UID    int
}

// ServiceInfo is the subset of `launchctl print` output we consume.
type ServiceInfo struct {
	Label     string
	PlistPath string
	PID       int
	State     string
	LastExit  int
}

func (s *Service) domain() string {
	return fmt.Sprintf("gui/%d", s.UID)
}

// Load bootstraps a plist into the user GUI domain.
func (s *Service) Load(ctx context.Context, plistPath string) error {
	return s.Runner.Run(ctx, "launchctl", []string{"bootstrap", s.domain(), plistPath}, "", io.Discard, io.Discard)
}

// Bootout removes a service from the user GUI domain.
func (s *Service) Bootout(ctx context.Context, label string) error {
	target := s.domain() + "/" + label
	return s.Runner.Run(ctx, "launchctl", []string{"bootout", target}, "", io.Discard, io.Discard)
}

// Print queries launchctl for a service's state. Returns a zero-PID
// ServiceInfo (and nil error) when the service isn't loaded — that's
// not a programming error, it's just "not running".
func (s *Service) Print(ctx context.Context, label string) (ServiceInfo, error) {
	target := s.domain() + "/" + label
	var buf bytes.Buffer
	if err := s.Runner.Run(ctx, "launchctl", []string{"print", target}, "", &buf, io.Discard); err != nil {
		// launchctl exits nonzero when the service isn't loaded.
		// That's a normal "stopped" state, not a failure.
		return ServiceInfo{Label: label}, nil
	}
	return parsePrintOutput(label, buf.String()), nil
}

// parsePrintOutput extracts {state, pid, last exit code} from `launchctl
// print`'s human-readable output. Missing keys leave their fields zero.
func parsePrintOutput(label, output string) ServiceInfo {
	info := ServiceInfo{Label: label}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "state = "):
			info.State = strings.TrimPrefix(line, "state = ")
		case strings.HasPrefix(line, "pid = "):
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "pid = ")); err == nil {
				info.PID = n
			}
		case strings.HasPrefix(line, "last exit code = "):
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "last exit code = ")); err == nil {
				info.LastExit = n
			}
		}
	}
	return info
}
