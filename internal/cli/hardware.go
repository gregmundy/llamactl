package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newHardwareCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:           "hardware",
		Short:         "Detect chip, RAM, GPU memory, OS version; cache to hardware.json",
		SilenceErrors: true, // main.go is the single error printer
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHardware(cmd.Context(), deps)
		},
	}
}

func runHardware(ctx context.Context, deps *Deps) error {
	info, err := deps.HardwareDetector.Detect(ctx)
	if err != nil {
		return fmt.Errorf("detect hardware: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(deps.HardwareJSONPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hardware.json: %w", err)
	}
	if err := os.WriteFile(deps.HardwareJSONPath, b, 0o644); err != nil {
		return fmt.Errorf("write hardware.json: %w", err)
	}

	fmt.Fprintf(deps.Stdout, "Chip:        %s\n", nonEmpty(info.Chip, "unknown"))
	fmt.Fprintf(deps.Stdout, "RAM:         %s\n", humanBytes(info.RAMBytes))
	fmt.Fprintf(deps.Stdout, "OS:          %s\n", nonEmpty(info.OSVersion, "unknown"))
	if info.IogpuWiredLimitMB > 0 {
		fmt.Fprintf(deps.Stdout, "iogpu cap:   %d MB\n", info.IogpuWiredLimitMB)
	} else {
		fmt.Fprintln(deps.Stdout, "iogpu cap:   unset (default ~75% of RAM)")
	}
	fmt.Fprintf(deps.Stdout, "Hypervisor:  %v\n", info.HypervisorPresent)
	fmt.Fprintf(deps.Stdout, "Metal GPU:   %v\n", info.MetalDeviceDetected)
	fmt.Fprintf(deps.Stdout, "Saved to:    %s\n", deps.HardwareJSONPath)
	return nil
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func humanBytes(n uint64) string {
	if n == 0 {
		return "unknown"
	}
	gb := float64(n) / (1 << 30)
	return fmt.Sprintf("%.0f GB", gb)
}
