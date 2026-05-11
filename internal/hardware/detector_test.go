package hardware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// fakeRunner replays canned stdout per (name, args[0]) and never invokes a
// real binary. Unmatched calls fail the test.
type fakeRunner struct {
	t       *testing.T
	outputs map[string]string // key: name + " " + args[0]
	errs    map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ string, stdout, stderr io.Writer) error {
	key := name
	if len(args) > 0 {
		key = name + " " + args[0]
	}
	if err, ok := f.errs[key]; ok {
		return err
	}
	out, ok := f.outputs[key]
	if !ok {
		f.t.Fatalf("unexpected runner call: %s %v", name, args)
	}
	_, _ = io.WriteString(stdout, out)
	_ = stderr
	return nil
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestDetect_ReturnsZeroValueWhenAllCommandsFail(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		errs: map[string]error{
			"system_profiler SPHardwareDataType": errors.New("fail"),
			"system_profiler SPDisplaysDataType": errors.New("fail"),
			"sysctl hw.memsize":                  errors.New("fail"),
			"sysctl iogpu.wired_limit_mb":        errors.New("fail"),
			"sysctl kern.hv_vmm_present":         errors.New("fail"),
		},
	}
	d := &Detector{Runner: runner}
	info, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect should not error on subcommand failures: %v", err)
	}
	if info.Chip != "" || info.RAMBytes != 0 {
		t.Fatalf("expected zero values, got %+v", info)
	}
}

func TestInfo_JSONRoundTrip(t *testing.T) {
	in := Info{Chip: "Apple M2 Pro", ChipGen: "M2", RAMBytes: 32 * 1024 * 1024 * 1024, IogpuWiredLimitMB: 0, HypervisorPresent: false, MetalDeviceDetected: true}
	b, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var out Info
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", out, in)
	}
}

func TestDetect_ParsesChipFromSystemProfiler(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		outputs: map[string]string{
			"system_profiler SPHardwareDataType": readFixture(t, "sphardware_m2pro.json"),
		},
		errs: map[string]error{
			"system_profiler SPDisplaysDataType": errors.New("not needed"),
			"sysctl hw.memsize":                  errors.New("not needed"),
			"sysctl iogpu.wired_limit_mb":        errors.New("not needed"),
			"sysctl kern.hv_vmm_present":         errors.New("not needed"),
			"sw_vers -productVersion":            errors.New("not needed"),
		},
	}
	info, _ := (&Detector{Runner: runner}).Detect(context.Background())
	if info.Chip != "Apple M2 Pro" {
		t.Fatalf("Chip = %q, want %q", info.Chip, "Apple M2 Pro")
	}
	if info.ChipGen != "M2" {
		t.Fatalf("ChipGen = %q, want %q", info.ChipGen, "M2")
	}
}

func TestParseChipGen(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Apple M1", "M1"},
		{"Apple M1 Pro", "M1"},
		{"Apple M2 Max", "M2"},
		{"Apple M3 Ultra", "M3"},
		{"Apple M4", "M4"},
		{"Unknown", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := parseChipGen(c.in)
		if got != c.want {
			t.Errorf("parseChipGen(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
