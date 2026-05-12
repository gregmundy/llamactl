package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregmundy/llamactl/internal/runner"
	"github.com/spf13/cobra"
)

type latestFetcher func(ctx context.Context, refresh bool) (string, error)
type executableFn func() (string, error)

func newUpdateCmd(d *Deps) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Upgrade llamactl to the latest published version (Homebrew)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cachePath := versionCachePath(d)
			now := d.Now
			fetcher := func(ctx context.Context, ref bool) (string, error) {
				return fetchLatestVersion(ctx, CaskURL, cachePath, ref, now)
			}
			return runUpdate(cmd.Context(), d, cmd.Root().Version, refresh, fetcher, os.Executable, d.Runner)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "bypass the 24h version-check cache")
	return cmd
}

// versionCachePath returns the on-disk path for the latest-version JSON cache.
// Uses d.HFCacheDir as the llamactl cache root (typically ~/.cache/llamactl).
func versionCachePath(d *Deps) string {
	if d.HFCacheDir == "" {
		return ""
	}
	return filepath.Join(d.HFCacheDir, "last-version-check.json")
}

func runUpdate(ctx context.Context, d *Deps, currentVersion string, refresh bool,
	fetch latestFetcher, executable executableFn, run runner.CommandRunner) error {

	latest, err := fetch(ctx, refresh)
	if err != nil {
		return fmt.Errorf("fetch latest version: %w", err)
	}

	if currentVersion == "dev" || currentVersion == "" {
		fmt.Fprintln(d.Stdout, "current: dev (local build)")
		fmt.Fprintf(d.Stdout, "latest:  %s\n", normalizeWithV(latest))
		fmt.Fprintln(d.Stdout, "dev builds don't auto-update; install via brew or `go install ...@latest`")
		return nil
	}

	if !updateAvailable(currentVersion, latest) {
		fmt.Fprintf(d.Stdout, "already on latest (%s)\n", currentVersion)
		return nil
	}

	fmt.Fprintf(d.Stdout, "current: %s\n", currentVersion)
	fmt.Fprintf(d.Stdout, "latest:  %s\n", normalizeWithV(latest))

	execPath, _ := executable()
	if !isBrewInstall(execPath) {
		fmt.Fprintf(d.Stdout,
			"llamactl is not installed via Homebrew; the binary is at %s.\n"+
				"Upgrade with your installer (e.g., `go install github.com/gregmundy/llamactl/cmd/llamactl@latest`).\n",
			execPath)
		return nil
	}

	if run == nil {
		return fmt.Errorf("internal: no runner configured")
	}
	fmt.Fprintln(d.Stdout, "==> brew update")
	if err := run.Run(ctx, "brew", []string{"update"}, "", d.Stdout, d.Stderr); err != nil {
		return fmt.Errorf("brew update: %w", err)
	}
	fmt.Fprintln(d.Stdout, "==> brew upgrade gregmundy/tap/llamactl")
	if err := run.Run(ctx, "brew", []string{"upgrade", "gregmundy/tap/llamactl"}, "", d.Stdout, d.Stderr); err != nil {
		return fmt.Errorf("brew upgrade: %w", err)
	}
	fmt.Fprintln(d.Stdout, "done.")
	return nil
}

func isBrewInstall(path string) bool {
	return strings.HasPrefix(path, "/opt/homebrew/Caskroom/llamactl/") ||
		path == "/opt/homebrew/bin/llamactl" ||
		strings.HasPrefix(path, "/usr/local/Caskroom/llamactl/") ||
		path == "/usr/local/bin/llamactl"
}

func normalizeWithV(v string) string {
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
