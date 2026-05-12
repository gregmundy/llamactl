package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
)

const (
	fitHeadroomGB = 4.0
	fitDefaultCtx = 8192
)

// fitMinModelBytes filters out imatrix calibration shards and other small
// auxiliary GGUFs that match the quant regex but aren't actual model weights.
// 200 MiB is safely above imatrix shards (~100 MB) while still admitting
// legitimate sub-1B Q4_K_M files (e.g. qwen3-0.6b at ~600 MB).
const fitMinModelBytes = 200 << 20 // 200 MiB

var fitQuantRe = regexp.MustCompile(`(IQ\d+_[A-Z0-9_]+|Q\d+_[A-Z0-9_]+|Q\d+_0)`)

type fitRow struct {
	Repo      string  `json:"repo"`
	Quant     string  `json:"quant"`
	SizeGB    float64 `json:"size_gb"`
	Verdict   string  `json:"verdict"` // "ok", "tight", "too-big"
	FreeGB    float64 `json:"free_gb,omitempty"`
	DeficitGB float64 `json:"deficit_gb,omitempty"`
	Note      string  `json:"note,omitempty"`
	Downloads int     `json:"downloads,omitempty"`
	Likes     int     `json:"likes,omitempty"`
}

func newFitCmd(d *Deps) *cobra.Command {
	var install bool
	var ctxSize int
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "fit <query...>",
		Short: "Search HF and rank GGUF variants by fit on this host",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFit(cmd.Context(), d, strings.Join(args, " "), install, ctxSize, limit, asJSON)
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "install the top-ranked OK row")
	cmd.Flags().IntVar(&ctxSize, "ctx", fitDefaultCtx, "context size for KV-cache estimation")
	cmd.Flags().IntVar(&limit, "limit", 10, "cap rows shown")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}

func runFit(ctx context.Context, d *Deps, query string, install bool, ctxSize, limit int, asJSON bool) error {
	hw, err := ensureHardware(ctx, d)
	if err != nil {
		return err
	}
	hits, err := d.HFClient.Search(ctx, query)
	if err != nil {
		return fmt.Errorf("hf search: %w", err)
	}
	usable := models.GpuAddressableGB(hw) - models.OSOverheadGB - models.HeadroomGB

	var rows []fitRow
	for _, hit := range hits {
		repo, err := d.HFClient.RepoInfo(ctx, hit.ID)
		if err != nil {
			continue
		}
		paramsB := models.ParseParamCountFromRepo(hit.ID)
		if paramsB == 0 {
			paramsB = 13 // conservative fallback
		}
		arch := models.Arch("")
		for _, m := range models.PreferredIDs {
			if strings.EqualFold(m.HFRepo, hit.ID) {
				arch = m.Arch
				break
			}
		}
		for _, f := range repo.Siblings {
			if f.LFS == nil || f.LFS.Size < fitMinModelBytes {
				continue
			}
			lower := strings.ToLower(f.RFilename)
			if !strings.HasSuffix(lower, ".gguf") {
				continue
			}
			if strings.Contains(lower, "mmproj") {
				// Multimodal projector (CLIP/vision component) — not a standalone model.
				continue
			}
			if strings.Contains(f.RFilename, "-of-") {
				// Multi-shard GGUFs (e.g. "model-00001-of-00002.gguf") are not supported
				// by `add` today — skip them so `--install` doesn't pick something it
				// can't install.
				continue
			}
			q := fitQuantRe.FindString(f.RFilename)
			if q == "" {
				continue
			}
			sizeGB := float64(f.LFS.Size) / 1e9
			kvGB := models.KVCacheGB(arch, paramsB, ctxSize)
			if kvGB == 0 {
				kvGB = 1.0
			}
			total := sizeGB + kvGB
			row := fitRow{Repo: hit.ID, Quant: q, SizeGB: sizeGB, Downloads: hit.Downloads, Likes: hit.Likes}
			switch {
			case usable-total >= fitHeadroomGB:
				row.Verdict = "ok"
				row.FreeGB = usable - total
				row.Note = fmt.Sprintf("%.0f GB free", row.FreeGB)
			case usable >= total:
				row.Verdict = "tight"
				row.FreeGB = usable - total
				row.Note = "tight headroom"
			default:
				row.Verdict = "too-big"
				row.DeficitGB = total - usable
				row.Note = fmt.Sprintf("need %.0f GB more", row.DeficitGB)
			}
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		fmt.Fprintln(d.Stdout, "no GGUF repos matched")
		return nil
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return fitRank(rows[i]) > fitRank(rows[j])
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}

	if install {
		for _, r := range rows {
			if r.Verdict == "ok" {
				return runAdd(ctx, d, r.Repo, r.Quant, ctxSize)
			}
		}
		return fmt.Errorf("%w: no fit candidate for %q; run `llamactl fit %s` to see all options", ErrUserError, query, query)
	}

	if asJSON {
		return json.NewEncoder(d.Stdout).Encode(rows)
	}
	return renderFitTable(d.Stdout, rows)
}

func fitRank(r fitRow) float64 {
	switch r.Verdict {
	case "ok":
		// Within ✓: weight by downloads (canonical repos surface first);
		// tiebreak on size (higher fidelity preferred among equally-popular).
		return 100_000_000 + float64(r.Downloads) + r.SizeGB
	case "tight":
		return 100 - r.SizeGB
	default:
		return -r.DeficitGB
	}
}

func renderFitTable(w io.Writer, rows []fitRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RECOMMENDED\tREPO\tQUANT\tSIZE\tVERDICT\tNOTES")
	for _, r := range rows {
		var sym string
		switch r.Verdict {
		case "ok":
			sym = "   ✓"
		case "tight":
			sym = "   ⚠"
		default:
			sym = "   ✗"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f GB\t%s\t%s\n", sym, r.Repo, r.Quant, r.SizeGB, r.Verdict, r.Note)
	}
	return tw.Flush()
}
