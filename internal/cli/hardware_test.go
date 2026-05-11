package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregmundy/llamactl/internal/hardware"
)

type fakeDetector struct {
	info hardware.Info
	err  error
}

func (f *fakeDetector) Detect(_ context.Context) (hardware.Info, error) {
	return f.info, f.err
}

func TestHardware_WritesJSONAndPrints(t *testing.T) {
	tmp := t.TempDir()
	deps := &Deps{
		HardwareDetector: &fakeDetector{info: hardware.Info{
			Chip:                "Apple M2 Pro",
			ChipGen:             "M2",
			RAMBytes:            32 * 1024 * 1024 * 1024,
			IogpuWiredLimitMB:   0,
			HypervisorPresent:   false,
			MetalDeviceDetected: true,
			OSVersion:           "14.4.1",
		}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		Now:              func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	out, _, err := runRoot(t, deps, "hardware")
	if err != nil {
		t.Fatalf("hardware: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Apple M2 Pro") {
		t.Errorf("expected chip in output, got %q", out)
	}
	if !strings.Contains(out, "32 GB") {
		t.Errorf("expected RAM in output, got %q", out)
	}

	b, err := os.ReadFile(filepath.Join(tmp, "hardware.json"))
	if err != nil {
		t.Fatalf("read hardware.json: %v", err)
	}
	var got hardware.Info
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Chip != "Apple M2 Pro" {
		t.Errorf("persisted Chip = %q", got.Chip)
	}
}

func TestHardware_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	deps := &Deps{
		HardwareDetector: &fakeDetector{info: hardware.Info{Chip: "Apple M2"}},
		HardwareJSONPath: filepath.Join(tmp, "hardware.json"),
		Now:              func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	if _, _, err := runRoot(t, deps, "hardware"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, _, err := runRoot(t, deps, "hardware"); err != nil {
		t.Fatalf("second run: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(tmp, "hardware.json"))
	var info hardware.Info
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatalf("unmarshal after rerun: %v", err)
	}
}
