package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ErrProcessNotFound is returned by Inspector when `ps` exits nonzero
// for the requested pid (process gone or never existed).
var ErrProcessNotFound = errors.New("process not found")

// CommandRunner is the subprocess seam, locally redeclared (same
// pattern as internal/hardware/internal/server).
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin string, stdout, stderr io.Writer) error
}

// Inspector queries the kernel about a running pid via `ps`.
type Inspector struct {
	Runner CommandRunner
}

// RSS returns the resident-set size of pid in bytes.
func (i *Inspector) RSS(pid int) (int64, error) {
	var buf bytes.Buffer
	args := []string{"-o", "rss=", "-p", strconv.Itoa(pid)}
	if err := i.Runner.Run(context.Background(), "ps", args, "", &buf, io.Discard); err != nil {
		return 0, ErrProcessNotFound
	}
	field := strings.TrimSpace(buf.String())
	if field == "" {
		return 0, ErrProcessNotFound
	}
	kb, err := strconv.ParseInt(field, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse rss %q: %w", field, err)
	}
	return kb * 1024, nil
}

// Uptime returns elapsed wall time since pid started.
// `ps -o etime=` formats as [[dd-]hh:]mm:ss.
func (i *Inspector) Uptime(pid int) (time.Duration, error) {
	var buf bytes.Buffer
	args := []string{"-o", "etime=", "-p", strconv.Itoa(pid)}
	if err := i.Runner.Run(context.Background(), "ps", args, "", &buf, io.Discard); err != nil {
		return 0, ErrProcessNotFound
	}
	return parseEtime(strings.TrimSpace(buf.String()))
}

// parseEtime parses ps's etime field. Examples:
//
//	"05:23"        -> 5m 23s
//	"1:05:23"      -> 1h 5m 23s
//	"2-01:05:23"   -> 2d 1h 5m 23s
func parseEtime(s string) (time.Duration, error) {
	var days int
	if i := strings.Index(s, "-"); i != -1 {
		d, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("parse etime days %q: %w", s, err)
		}
		days = d
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	var h, m, sec int
	switch len(parts) {
	case 2:
		m, _ = strconv.Atoi(parts[0])
		sec, _ = strconv.Atoi(parts[1])
	case 3:
		h, _ = strconv.Atoi(parts[0])
		m, _ = strconv.Atoi(parts[1])
		sec, _ = strconv.Atoi(parts[2])
	default:
		return 0, fmt.Errorf("parse etime %q: unexpected format", s)
	}
	return time.Duration(days)*24*time.Hour +
		time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute +
		time.Duration(sec)*time.Second, nil
}
