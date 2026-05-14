package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newStopCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [<run-name>]",
		Short: "Stop a detached llamactl service (or all services if no name)",
		Long: `Stop a detached llamactl service.

Without arguments, stops every running llamactl service. With a run-name,
stops just that one. The default run-name is the model id, so single-
instance users can still pass the model id directly.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runStopAll(cmd.Context(), d)
			}
			return runStopOne(cmd.Context(), d, args[0])
		},
	}
}

func runStopAll(ctx context.Context, d *Deps) error {
	services, err := d.LaunchdService.List(ctx)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		fmt.Fprintln(d.Stdout, "no llamactl services running")
		return nil
	}
	for _, svc := range services {
		name := strings.TrimPrefix(svc.Label, "com.llamactl.")
		if err := stopOne(ctx, d, svc.Label, svc.PlistPath, name); err != nil {
			fmt.Fprintf(d.Stderr, "llamactl: warning: stop %s: %v\n", name, err)
		}
	}
	return nil
}

func runStopOne(ctx context.Context, d *Deps, name string) error {
	label := "com.llamactl." + name
	plistPath := filepath.Join(d.LaunchAgentsDir, label+".plist")
	if _, err := os.Stat(plistPath); err != nil {
		return fmt.Errorf("%w: no detached service named %q (looked at %s)", ErrUserError, name, plistPath)
	}
	return stopOne(ctx, d, label, plistPath, name)
}

func stopOne(ctx context.Context, d *Deps, label, plistPath, name string) error {
	_ = d.LaunchdService.Bootout(ctx, label) // best-effort
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	fmt.Fprintf(d.Stdout, "stopped %s and removed %s\n", name, plistPath)
	return nil
}
