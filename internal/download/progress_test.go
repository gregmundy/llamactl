package download

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProgressEmitsCarriageReturnUpdates(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	p := &Progress{
		Out:         &buf,
		Total:       10 * 1024 * 1024, // 10 MiB
		Now:         func() time.Time { now = now.Add(250 * time.Millisecond); return now },
		IsTTY:       true,
		MinInterval: 200 * time.Millisecond,
	}
	for i := 0; i < 10; i++ {
		_, _ = p.Write(make([]byte, 1024*1024)) // 1 MiB each
	}
	p.Finish()
	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Errorf("output should contain CR updates; got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("Finish should emit trailing newline; got %q", out[len(out)-5:])
	}
}

func TestProgressSuppressedWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{Out: &buf, Total: 1024, IsTTY: false}
	_, _ = p.Write(make([]byte, 1024))
	p.Finish()
	if buf.Len() != 0 {
		t.Errorf("expected no output when not TTY; got %q", buf.String())
	}
}
