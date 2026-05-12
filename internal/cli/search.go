package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
)

func newSearchCmd(d *Deps) *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search HuggingFace for GGUF repos (preferred IDs marked with *)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), d, args[0], refresh)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "bypass the search cache")
	return cmd
}

const searchResultLimit = 25

func runSearch(ctx context.Context, d *Deps, query string, refresh bool) error {
	var hits []hf.SearchHit
	var err error
	if refresh {
		hits, err = d.HFClient.SearchRefresh(ctx, query)
	} else {
		hits, err = d.HFClient.Search(ctx, query)
	}
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		fmt.Fprintln(d.Stdout, "no matches")
		return nil
	}
	if len(hits) > searchResultLimit {
		hits = hits[:searchResultLimit]
	}

	// Reverse index: HF repo -> preferred Model.
	byRepo := make(map[string]models.Model, len(models.PreferredIDs))
	for _, m := range models.PreferredIDs {
		byRepo[m.HFRepo] = m
	}

	type row struct {
		preferred bool
		id        string // short id (preferred) or full HF repo path
		params    string
		quants    string
		repo      string
	}
	rows := make([]row, 0, len(hits))
	for _, h := range hits {
		var r row
		if m, ok := byRepo[h.ID]; ok {
			r = row{
				preferred: true,
				id:        m.ID,
				params:    fmt.Sprintf("%gB", m.ParamsB),
				repo:      m.HFRepo,
			}
		} else {
			r = row{
				preferred: false,
				id:        h.ID,
				params:    "?",
				repo:      h.ID,
			}
		}
		repo, rerr := d.HFClient.RepoInfo(ctx, r.repo)
		if rerr == nil {
			r.quants = strings.Join(availableQuantsFromSiblings(repo.Siblings), ",")
		} else {
			r.quants = "(unknown)"
		}
		rows = append(rows, r)
	}

	// Sort: preferred first (alphabetical by id), then non-preferred
	// (preserve HF order, which is downloads-descending).
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].preferred != rows[j].preferred {
			return rows[i].preferred
		}
		if rows[i].preferred {
			return rows[i].id < rows[j].id
		}
		return false
	})

	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "\tMODEL\tPARAMS\tQUANTS\tREPO")
	for _, r := range rows {
		marker := " "
		if r.preferred {
			marker = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", marker, r.id, r.params, r.quants, r.repo)
	}
	return tw.Flush()
}

// availableQuantsFromSiblings extracts which canonical quants appear in the
// repo's .gguf siblings (case-insensitive match). Returned in PreferenceOrder.
func availableQuantsFromSiblings(files []hf.File) []string {
	found := make(map[models.Quant]bool)
	for _, f := range files {
		low := strings.ToLower(f.RFilename)
		if !strings.HasSuffix(low, ".gguf") {
			continue
		}
		for _, q := range models.PreferenceOrder {
			if strings.Contains(low, strings.ToLower(string(q))) {
				found[q] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for _, q := range models.PreferenceOrder {
		if found[q] {
			out = append(out, string(q))
		}
	}
	return out
}
