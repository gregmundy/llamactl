package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/spf13/cobra"
)

func newCacheCmd(d *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the HuggingFace API response cache",
	}
	cmd.AddCommand(newCachePruneCmd(d))
	return cmd
}

func newCachePruneCmd(d *Deps) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove stale HuggingFace cache entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCachePrune(d, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "remove all cache entries, not just stale ones")
	return cmd
}

func runCachePrune(d *Deps, all bool) error {
	if d.HFCacheDir == "" {
		return fmt.Errorf("%w: HF cache dir not configured", ErrUserError)
	}
	n, err := walkCacheAndPrune(d.HFCacheDir, all)
	if err != nil {
		return err
	}
	if all {
		fmt.Fprintf(d.Stdout, "removed %d cache file(s)\n", n)
	} else {
		fmt.Fprintf(d.Stdout, "removed %d stale cache file(s)\n", n)
	}
	cache := hf.NewCache(d.HFCacheDir)
	_ = cache.GCEmptyNamespaces()
	return nil
}

func walkCacheAndPrune(root string, all bool) (int, error) {
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	removed := 0
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if e.IsDir() {
			return nil
		}
		if !all {
			info, ierr := e.Info()
			if ierr != nil {
				return nil
			}
			if !info.ModTime().Before(cutoff) {
				return nil
			}
		}
		_ = os.Remove(p)
		if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
			removed++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return removed, err
	}
	return removed, nil
}
