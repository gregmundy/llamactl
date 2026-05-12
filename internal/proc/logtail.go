package proc

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TailRate scans the tail of a llama-server log file and computes a
// token-weighted average tokens/sec from the eval lines.
//
// Note on the "rolling 60s window" PRD wording: llama-server's standard
// per-request log lines don't carry timestamps in every build, so we
// approximate "rolling" as "last maxReadBytes scanned from EOF". The
// window argument is accepted for API stability and reserved for a
// future build that does emit timestamps.
type TailRate struct{}

// maxReadBytes caps how far back from EOF we scan. 256 KiB easily
// covers the last few hundred eval lines on a busy server.
const maxReadBytes = 256 << 10

// evalLineRe matches both phrasings llama-server uses:
//
//	"eval time =   1000.00 ms /  100 tokens (   10.00 ms per token,    100.00 tokens per second)"
//	"prompt eval time =   500.00 ms /   50 tokens (   10.00 ms per token,    100.00 tokens per second)"
//
// Captures: ms (group 1), tokens (group 2).
var evalLineRe = regexp.MustCompile(
	`(?:prompt )?eval time\s*=\s*([0-9.]+)\s*ms\s*/\s*([0-9]+)\s+tokens`)

// Rate reads the tail of logPath and returns the token-weighted average
// tokens/sec across matched eval lines. Missing files and empty files
// return (0, nil).
func (r *TailRate) Rate(logPath string, window time.Duration, now time.Time) (float64, error) {
	_ = window // see TailRate doc comment

	f, err := os.Open(logPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := fi.Size()
	if size == 0 {
		return 0, nil
	}
	offset := int64(0)
	if size > maxReadBytes {
		offset = size - maxReadBytes
	}
	buf := make([]byte, size-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return 0, err
	}

	var totalTokens int64
	var totalMs float64
	for _, line := range strings.Split(string(buf), "\n") {
		m := evalLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ms, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		tokens, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			continue
		}
		totalMs += ms
		totalTokens += tokens
	}
	if totalTokens == 0 || totalMs == 0 {
		return 0, nil
	}
	return float64(totalTokens) / (totalMs / 1000.0), nil
}
