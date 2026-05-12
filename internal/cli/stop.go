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
		Use:   "stop [<model-id>]",
		Short: "Stop a detached llamactl service (or all services if no id)",
		Args:  cobra.MaximumNArgs(1),
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
		id := strings.TrimPrefix(svc.Label, "com.llamactl.")
		if err := stopOne(ctx, d, svc.Label, svc.PlistPath, id); err != nil {
			fmt.Fprintf(d.Stderr, "llamactl: warning: stop %s: %v\n", id, err)
		}
	}
	return nil
}

func runStopOne(ctx context.Context, d *Deps, id string) error {
	label := "com.llamactl." + id
	plistPath := filepath.Join(d.LaunchAgentsDir, label+".plist")
	if _, err := os.Stat(plistPath); err != nil {
		return fmt.Errorf("%w: no detached service for %q (looked at %s)", ErrUserError, id, plistPath)
	}
	return stopOne(ctx, d, label, plistPath, id)
}

func stopOne(ctx context.Context, d *Deps, label, plistPath, id string) error {
	_ = d.LaunchdService.Bootout(ctx, label) // best-effort
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	fmt.Fprintf(d.Stdout, "stopped %s and removed %s\n", id, plistPath)
	return nil
}
