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
		Short: "Search whitelisted models on HuggingFace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), d, args[0], refresh)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "bypass the search cache")
	return cmd
}

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

	// Filter to whitelisted repos. Build a reverse index repo -> model once.
	byRepo := make(map[string]models.Model, len(models.PreferredIDs))
	for _, m := range models.PreferredIDs {
		byRepo[m.HFRepo] = m
	}
	matched := make([]models.Model, 0, len(hits))
	seen := make(map[string]bool)
	for _, h := range hits {
		if m, ok := byRepo[h.ID]; ok && !seen[m.ID] {
			matched = append(matched, m)
			seen[m.ID] = true
		}
	}
	if len(matched) == 0 {
		fmt.Fprintln(d.Stdout, "no matches")
		return nil
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL-ID\tPARAMS\tQUANTS\tREPO")
	for _, m := range matched {
		repo, rerr := d.HFClient.RepoInfo(ctx, m.HFRepo)
		quants := "(unknown)"
		if rerr == nil {
			quants = strings.Join(availableQuantsFromSiblings(repo.Siblings), ",")
		}
		fmt.Fprintf(tw, "%s\t%dB\t%s\t%s\n", m.ID, m.ParamsB, quants, m.HFRepo)
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
