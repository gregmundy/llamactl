package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/spf13/cobra"
)

func newTelemetryCmd(d *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage the telemetry sidecar daemon",
	}
	cmd.AddCommand(newTelemetryEnableCmd(d))
	cmd.AddCommand(newTelemetryDisableCmd(d))
	cmd.AddCommand(newTelemetryStatusCmd(d))
	return cmd
}

func newTelemetryEnableCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Start the telemetry sidecar (persistent across reboots)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTelemetryEnable(cmd.Context(), d)
		},
	}
}

func runTelemetryEnable(ctx context.Context, d *Deps) error {
	host := d.Config.TelemetryHost
	if host == "" {
		host = "0.0.0.0"
	}
	apiKey := d.Getenv("LLAMACTL_API_KEY")
	if apiKey == "" {
		apiKey = d.Config.APIKey
	}
	if isPublicHostStr(host) && apiKey == "" {
		return fmt.Errorf("%w: telemetry_host=%s requires api_key; run `llamactl config set api_key <token>` or `llamactl config set telemetry_host 127.0.0.1`",
			ErrUserError, host)
	}

	binPath, err := resolveTelemetrydBinary(d)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	if err := os.MkdirAll(d.LogsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir Logs: %w", err)
	}

	home, err := d.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	spec := launchd.TelemetrydSpec{
		Label:      launchd.TelemetrydLabel,
		BinaryPath: binPath,
		LogPath:    filepath.Join(d.LogsDir, "telemetryd.log"),
		WorkingDir: home,
	}
	body, err := launchd.RenderTelemetryd(spec)
	if err != nil {
		return err
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
	tmp := plistPath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write plist tmp: %w", err)
	}
	if err := os.Rename(tmp, plistPath); err != nil {
		return fmt.Errorf("rename plist: %w", err)
	}

	if existing, _ := d.LaunchdService.Print(ctx, launchd.TelemetrydLabel); existing.PID != 0 {
		_ = d.LaunchdService.Bootout(ctx, launchd.TelemetrydLabel)
	}
	if err := d.LaunchdService.Load(ctx, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}
	port := d.Config.TelemetryPort
	if port == 0 {
		port = 18080
	}
	fmt.Fprintf(d.Stdout, "telemetryd started on http://%s:%d\n", host, port)
	return nil
}

// resolveTelemetrydBinary finds the llamactl-telemetryd binary via PATH;
// falls back to the directory of the running llamactl binary.
func resolveTelemetrydBinary(d *Deps) (string, error) {
	if d.LookPath != nil {
		if p, err := d.LookPath("llamactl-telemetryd"); err == nil {
			return p, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "llamactl-telemetryd")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: cannot find llamactl-telemetryd binary in PATH", ErrUserError)
}

func newTelemetryDisableCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Stop and remove the telemetry sidecar",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTelemetryDisable(cmd.Context(), d)
		},
	}
}

func runTelemetryDisable(ctx context.Context, d *Deps) error {
	_ = d.LaunchdService.Bootout(ctx, launchd.TelemetrydLabel)
	plistPath := filepath.Join(d.LaunchAgentsDir, launchd.TelemetrydLabel+".plist")
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	fmt.Fprintln(d.Stdout, "telemetryd stopped")
	return nil
}

func newTelemetryStatusCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show telemetry sidecar status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTelemetryStatus(cmd.Context(), d)
		},
	}
}

func runTelemetryStatus(ctx context.Context, d *Deps) error {
	info, _ := d.LaunchdService.Print(ctx, launchd.TelemetrydLabel)
	if info.PID == 0 {
		fmt.Fprintln(d.Stdout, "telemetryd: stopped")
		return nil
	}
	port := d.Config.TelemetryPort
	if port == 0 {
		port = 18080
	}
	host := d.Config.TelemetryHost
	if host == "" {
		host = "0.0.0.0"
	}
	fmt.Fprintf(d.Stdout, "telemetryd: running (pid=%d, host=%s, port=%d)\n", info.PID, host, port)
	return nil
}

// isPublicHostStr reports whether h binds to a non-loopback address.
func isPublicHostStr(h string) bool {
	switch h {
	case "127.0.0.1", "::1", "localhost":
		return false
	}
	return true
}
