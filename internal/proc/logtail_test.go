package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLog(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRateMissingFileReturnsZero(t *testing.T) {
	r := &TailRate{}
	got, err := r.Rate("/no/such/file.log", time.Minute, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got != 0 {
		t.Errorf("got %f, want 0", got)
	}
}

func TestRateEmptyFileReturnsZero(t *testing.T) {
	path := writeLog(t, nil)
	r := &TailRate{}
	got, err := r.Rate(path, time.Minute, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got != 0 {
		t.Errorf("got %f, want 0", got)
	}
}

func TestRateNoEvalLinesReturnsZero(t *testing.T) {
	path := writeLog(t, []string{
		"some unrelated log line",
		"another line",
		"loaded model qwen2.5-7b",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Minute, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got != 0 {
		t.Errorf("got %f, want 0", got)
	}
}

func TestRateParsesEvalLine(t *testing.T) {
	path := writeLog(t, []string{
		"eval time =    1000.00 ms /   100 tokens (   10.00 ms per token,   100.00 tokens per second)",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 99 || got > 101 {
		t.Errorf("got %f, want ~100", got)
	}
}

func TestRateWeightedAverageMultipleLines(t *testing.T) {
	// Sample A: 100 tokens in 1.0s → 100 t/s
	// Sample B: 200 tokens in 4.0s →  50 t/s
	// Weighted avg = (100+200) / (1.0+4.0) = 60 t/s
	path := writeLog(t, []string{
		"eval time =    1000.00 ms /   100 tokens (   10.00 ms per token,   100.00 tokens per second)",
		"eval time =    4000.00 ms /   200 tokens (   20.00 ms per token,    50.00 tokens per second)",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 59 || got > 61 {
		t.Errorf("got %f, want ~60 (weighted)", got)
	}
}

func TestRatePromptEvalAlsoParsed(t *testing.T) {
	path := writeLog(t, []string{
		"prompt eval time =    500.00 ms /    50 tokens (   10.00 ms per token,   100.00 tokens per second)",
	})
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 99 || got > 101 {
		t.Errorf("got %f, want ~100", got)
	}
}

func TestRateRespectsWindow(t *testing.T) {
	// Generate enough preceding garbage to exceed maxReadBytes (256 KiB),
	// then a single in-window eval line at the end. The line in window
	// should still be picked up because we scan from the end.
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("garbage line %d %s", i,
			"----------------------------------------------------------------"))
	}
	lines = append(lines,
		"eval time =    1000.00 ms /   100 tokens (   10.00 ms per token,   100.00 tokens per second)")
	path := writeLog(t, lines)
	r := &TailRate{}
	got, err := r.Rate(path, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if got < 99 || got > 101 {
		t.Errorf("got %f, want ~100", got)
	}
}
