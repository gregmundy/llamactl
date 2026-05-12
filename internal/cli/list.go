package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newListCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), d)
		},
	}
}

func runList(ctx context.Context, d *Deps) error {
	entries, err := d.ModelStore.List(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(d.Stdout, "no models installed")
		return nil
	}
	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL-ID\tQUANT\tPARAMS\tSIZE\tPATH\tADDED\tLAST-SERVED")
	for _, m := range entries {
		size := humanFileSize(m.SizeBytes)
		fi, statErr := d.FS.Stat(m.GGUFPath)
		switch {
		case statErr == nil:
			size = humanFileSize(fi.Size())
		case errors.Is(statErr, fs.ErrNotExist):
			size = "(missing)"
		default:
			size = "(stat err)"
		}
		params := "?"
		if m.ParamsB > 0 {
			params = fmt.Sprintf("%dB", m.ParamsB)
		}
		lastServed := ""
		if !m.LastServedAt.IsZero() {
			lastServed = m.LastServedAt.Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, m.Quant, params, size, m.GGUFPath, m.AddedAt.Format("2006-01-02"), lastServed)
	}
	return tw.Flush()
}
