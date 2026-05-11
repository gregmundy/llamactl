// Package hardware introspects an Apple Silicon Mac's chip, memory, GPU
// memory cap, and hypervisor state. Every probe is best-effort: a failing
// sysctl or system_profiler invocation leaves the corresponding Info field
// at its zero value rather than failing the whole detection. Doctor is the
// component that converts zero values into actionable error messages.
package hardware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// CommandRunner mirrors runner.CommandRunner — we redeclare locally to avoid
// importing the runner package from a leaf detection package. The cli wiring
// passes the same concrete ExecRunner to both.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Detector probes host hardware via CommandRunner. Construct with the
// production runner.ExecRunner in main.go, or a fake in tests.
type Detector struct {
	Runner CommandRunner
}

// Detect runs every probe and returns an Info populated with whatever
// succeeded. The error return is reserved for future use (e.g. catastrophic
// runner failure); today Detect never returns a non-nil error.
func (d *Detector) Detect(ctx context.Context) (Info, error) {
	var info Info
	d.probeChip(ctx, &info)
	d.probeRAM(ctx, &info)
	d.probeOSVersion(ctx, &info)
	d.probeIogpu(ctx, &info)
	d.probeHypervisor(ctx, &info)
	d.probeMetalDevice(ctx, &info)
	return info, nil
}

func (d *Detector) probeChip(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "system_profiler", []string{"SPHardwareDataType", "-json"}, "", &stdout, io.Discard); err != nil {
		return
	}
	var doc struct {
		SPHardwareDataType []struct {
			ChipType string `json:"chip_type"`
		} `json:"SPHardwareDataType"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return
	}
	if len(doc.SPHardwareDataType) == 0 {
		return
	}
	info.Chip = strings.TrimSpace(doc.SPHardwareDataType[0].ChipType)
	info.ChipGen = parseChipGen(info.Chip)
}

func (d *Detector) probeRAM(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sysctl", []string{"hw.memsize"}, "", &stdout, io.Discard); err != nil {
		return
	}
	// Output is "hw.memsize: 34359738368\n"
	parts := strings.SplitN(strings.TrimSpace(stdout.String()), ":", 2)
	if len(parts) != 2 {
		return
	}
	n, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return
	}
	info.RAMBytes = n
}

func (d *Detector) probeOSVersion(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sw_vers", []string{"-productVersion"}, "", &stdout, io.Discard); err != nil {
		return
	}
	info.OSVersion = strings.TrimSpace(stdout.String())
}

func (d *Detector) probeIogpu(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sysctl", []string{"iogpu.wired_limit_mb"}, "", &stdout, io.Discard); err != nil {
		return
	}
	s := strings.TrimSpace(stdout.String())
	if s == "" {
		return
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return
	}
	info.IogpuWiredLimitMB = n
}

func (d *Detector) probeHypervisor(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "sysctl", []string{"kern.hv_vmm_present"}, "", &stdout, io.Discard); err != nil {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(stdout.String()), ":", 2)
	if len(parts) != 2 {
		return
	}
	info.HypervisorPresent = strings.TrimSpace(parts[1]) == "1"
}

func (d *Detector) probeMetalDevice(ctx context.Context, info *Info) {
	var stdout bytes.Buffer
	if err := d.Runner.Run(ctx, "system_profiler", []string{"SPDisplaysDataType", "-json"}, "", &stdout, io.Discard); err != nil {
		return
	}
	var doc struct {
		SPDisplaysDataType []map[string]any `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return
	}
	// Any non-empty SPDisplaysDataType entry on Apple Silicon represents a
	// real Metal-capable GPU. VMs without GPU passthrough return an empty
	// array. This is the same heuristic the PRD §6.3 algorithm prescribes.
	info.MetalDeviceDetected = len(doc.SPDisplaysDataType) > 0
}

var chipGenRe = regexp.MustCompile(`\bM(\d+)\b`)

// parseChipGen extracts the generation token ("M1", "M2", ...) from an
// Apple Silicon chip name like "Apple M2 Pro". Returns "" when not Apple
// Silicon or unparseable.
func parseChipGen(chip string) string {
	m := chipGenRe.FindStringSubmatch(chip)
	if len(m) < 2 {
		return ""
	}
	return "M" + m[1]
}
