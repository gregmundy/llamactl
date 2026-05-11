package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
)

func newRemoveCmd(d *Deps) *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "remove <model-id>",
		Short: "Remove llamactl metadata for a model (use --purge to also delete the GGUF)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd.Context(), d, args[0], purge)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the shared GGUF file (best-effort cross-tool check)")
	return cmd
}

func runRemove(ctx context.Context, d *Deps, id string, purge bool) error {
	m, err := d.ModelStore.Get(ctx, id)
	if errors.Is(err, models.ErrNotFound) {
		return fmt.Errorf("%w: model %q is not installed", ErrUserError, id)
	}
	if err != nil {
		return err
	}

	if purge {
		if _, statErr := d.FS.Stat(m.GGUFPath + ".partial"); statErr == nil {
			return fmt.Errorf("%w: %s.partial exists — download in progress; aborting --purge", ErrUserError, m.GGUFPath)
		}
		fmt.Fprintf(d.Stderr,
			"llamactl: best-effort: cannot detect other tools' use of %s; deleting anyway\n",
			m.GGUFPath)
		if err := d.FS.Remove(m.GGUFPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove gguf: %w", err)
		}
		// Try to remove the (now likely empty) parent directory; ignore failure.
		_ = d.FS.Remove(filepath.Dir(m.GGUFPath))
	}

	if err := d.ModelStore.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}

	if purge {
		fmt.Fprintf(d.Stdout, "removed %s and deleted %s\n", id, m.GGUFPath)
	} else {
		fmt.Fprintf(d.Stdout, "removed %s metadata (GGUF preserved at %s)\n", id, m.GGUFPath)
	}
	return nil
}
