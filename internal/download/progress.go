// Package download orchestrates resumable, file-locked, SHA-verified
// downloads of single GGUF files. It depends on internal/hf for the
// network seam.
package download

import (
	"fmt"
	"io"
	"time"
)

// Progress is an io.Writer wrapper that emits a single-line progress
// indicator with carriage-return updates. No-op when IsTTY is false.
type Progress struct {
	Out         io.Writer
	Total       int64
	Initial     int64            // bytes already on disk before this run
	Now         func() time.Time // for tests; defaults to time.Now
	IsTTY       bool
	MinInterval time.Duration // suppress updates faster than this

	written int64
	start   time.Time
	last    time.Time
}

func (p *Progress) ensureInit() {
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.MinInterval == 0 {
		p.MinInterval = 250 * time.Millisecond
	}
	if p.start.IsZero() {
		p.start = p.Now()
		p.last = p.start.Add(-p.MinInterval) // force first update
	}
}

func (p *Progress) Write(b []byte) (int, error) {
	p.ensureInit()
	p.written += int64(len(b))
	if !p.IsTTY {
		return len(b), nil
	}
	now := p.Now()
	if now.Sub(p.last) < p.MinInterval {
		return len(b), nil
	}
	p.last = now
	p.emit(now)
	return len(b), nil
}

// Finish flushes a final update and a newline.
func (p *Progress) Finish() {
	if !p.IsTTY {
		return
	}
	p.ensureInit()
	p.emit(p.Now())
	fmt.Fprintln(p.Out)
}

func (p *Progress) emit(now time.Time) {
	done := p.Initial + p.written
	pct := 0.0
	if p.Total > 0 {
		pct = float64(done) / float64(p.Total) * 100
	}
	elapsed := now.Sub(p.start).Seconds()
	speedMiBs := 0.0
	if elapsed > 0 {
		speedMiBs = float64(p.written) / elapsed / (1024 * 1024)
	}
	etaStr := "--:--:--"
	if speedMiBs > 0 && p.Total > done {
		remBytes := float64(p.Total - done)
		etaSec := int(remBytes / (speedMiBs * 1024 * 1024))
		etaStr = fmt.Sprintf("%02d:%02d:%02d", etaSec/3600, (etaSec%3600)/60, etaSec%60)
	}
	fmt.Fprintf(p.Out, "\r%5.1f%%  %d/%d MiB  %.1f MiB/s  ETA %s",
		pct, done/(1024*1024), p.Total/(1024*1024), speedMiBs, etaStr)
}
