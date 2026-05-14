package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"

	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
)

// fitRepoInfoConcurrency caps the number of in-flight HF RepoInfo requests
// during a `fit` invocation. HF's anonymous rate limit is comfortably above
// this, and 8 keeps the worst-case wall time at ~one slow request rather
// than (slow × hits).
const fitRepoInfoConcurrency = 8

// isTTY reports whether w is a writer connected to a terminal. Used to
// gate progress feedback so piped output (e.g. `fit ... | jq`) stays
// clean and test buffers don't see the spinner bytes.
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

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
	var speculative bool
	cmd := &cobra.Command{
		Use:   "fit <query...>",
		Short: "Search HF and rank GGUF variants by fit on this host",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if speculative {
				return runFitSpeculative(cmd.Context(), d, strings.Join(args, " "), limit)
			}
			return runFit(cmd.Context(), d, strings.Join(args, " "), install, ctxSize, limit, asJSON)
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "install the top-ranked OK row")
	cmd.Flags().IntVar(&ctxSize, "ctx", fitDefaultCtx, "context size for KV-cache estimation")
	cmd.Flags().IntVar(&limit, "limit", 10, "cap rows shown")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	cmd.Flags().BoolVar(&speculative, "speculative", false, "list installed draft candidates for the named main model")

	// Tab completion: positional is a free-form HF query in the default
	// mode (no completion possible — would require HF API per keystroke),
	// but with --speculative it's an installed model id. Inspect the
	// already-parsed flag value to decide.
	cmd.ValidArgsFunction = func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if spec, _ := c.Flags().GetBool("speculative"); spec {
			return completeInstalledModels(d)(c, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
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

	// Parallelize the per-hit RepoInfo calls. HF is rate-friendly to small
	// bursts and a bounded worker pool caps wall time at ~one slow request
	// instead of summing across all hits. Progress feedback (when stderr is
	// a TTY) keeps users informed during the wait.
	var (
		rows      []fitRow
		rowsMu    sync.Mutex
		completed atomic.Int32
		total     = int32(len(hits))
		showProg  = isTTY(d.Stderr) && total > 1
	)
	if showProg {
		fmt.Fprintf(d.Stderr, "fetching repo info (0/%d)…", total)
	}
	emitProgress := func() {
		if !showProg {
			return
		}
		done := completed.Add(1)
		// \r returns to column 0; trailing spaces overwrite any prior longer line.
		fmt.Fprintf(d.Stderr, "\rfetching repo info (%d/%d)…     ", done, total)
	}

	sem := make(chan struct{}, fitRepoInfoConcurrency)
	var wg sync.WaitGroup
	for _, hit := range hits {
		wg.Add(1)
		sem <- struct{}{}
		go func(hit hf.SearchHit) {
			defer wg.Done()
			defer func() { <-sem }()
			defer emitProgress()

			repo, err := d.HFClient.RepoInfo(ctx, hit.ID)
			if err != nil {
				return
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
			var local []fitRow
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
				totalGB := sizeGB + kvGB
				row := fitRow{Repo: hit.ID, Quant: q, SizeGB: sizeGB, Downloads: hit.Downloads, Likes: hit.Likes}
				switch {
				case usable-totalGB >= fitHeadroomGB:
					row.Verdict = "ok"
					row.FreeGB = usable - totalGB
					row.Note = fmt.Sprintf("%.0f GB free", row.FreeGB)
				case usable >= totalGB:
					row.Verdict = "tight"
					row.FreeGB = usable - totalGB
					row.Note = "tight headroom"
				default:
					row.Verdict = "too-big"
					row.DeficitGB = totalGB - usable
					row.Note = fmt.Sprintf("need %.0f GB more", row.DeficitGB)
				}
				local = append(local, row)
			}
			if len(local) > 0 {
				rowsMu.Lock()
				rows = append(rows, local...)
				rowsMu.Unlock()
			}
		}(hit)
	}
	wg.Wait()
	if showProg {
		// Clear the progress line so the table output below isn't preceded
		// by leftover spinner text.
		fmt.Fprintf(d.Stderr, "\r%*s\r", 60, "")
	}

	if len(rows) == 0 {
		fmt.Fprintln(d.Stdout, "no GGUF repos matched")
		return nil
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return fitRank(rows[i]) > fitRank(rows[j])
	})

	// Per-repo dedupe with 60/40 bucketing.
	//
	// The best quant of each repo (primary) appears before any alternate
	// quant. Within each group the relative sort order is preserved so
	// popularity-weighting still determines which repo surfaces first.
	//
	// When --limit is small (< 5), primaries fill the whole table — users
	// looking at a short summary want repo diversity over quant variety.
	// When --limit is larger, reserve ~40% of slots for alternates of the
	// already-surfaced top repos so users can compare Q5/Q4/IQ3 variants
	// without scrolling past unrelated repos. If either bucket has fewer
	// candidates than its share, the other bucket absorbs the slack.
	seen := make(map[string]bool, len(rows))
	primary := make([]fitRow, 0, len(rows))
	alternates := make([]fitRow, 0, len(rows))
	for _, r := range rows {
		if !seen[r.Repo] {
			seen[r.Repo] = true
			primary = append(primary, r)
		} else {
			alternates = append(alternates, r)
		}
	}

	if limit >= 5 && len(alternates) > 0 {
		primaryQuota := limit * 60 / 100
		if primaryQuota < 1 {
			primaryQuota = 1
		}
		altQuota := limit - primaryQuota
		// Absorb slack: if either side has fewer rows than its quota,
		// give the surplus to the other side.
		if len(primary) < primaryQuota {
			altQuota += primaryQuota - len(primary)
			primaryQuota = len(primary)
		}
		if len(alternates) < altQuota {
			primaryQuota += altQuota - len(alternates)
			altQuota = len(alternates)
			if primaryQuota > len(primary) {
				primaryQuota = len(primary)
			}
		}
		rows = append(primary[:primaryQuota], alternates[:altQuota]...)
	} else {
		rows = append(primary, alternates...)
		if len(rows) > limit {
			rows = rows[:limit]
		}
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

// specRow is a fit row in speculative mode. The column meanings differ from
// the main `fitRow` shape but the verdict semantics come from
// models.SpeculativePair.
type specRow struct {
	DraftID       string  `json:"draft_id"`
	Arch          string  `json:"arch"`
	ParamsB       float64 `json:"params_b"`
	SizeRatio     float64 `json:"size_ratio"`
	CombinedRAMGB float64 `json:"combined_ram_gb"`
	Verdict       string  `json:"verdict"` // "ok", "ratio-low", "ratio-high", "refused"
	Reason        string  `json:"reason,omitempty"`
}

// runFitSpeculative is the --speculative branch of `llamactl fit`. The
// positional arg is the MAIN model id; candidates come from ModelStore.List.
func runFitSpeculative(ctx context.Context, d *Deps, mainID string, limit int) error {
	mainMeta, err := d.ModelStore.Get(ctx, mainID)
	if err != nil {
		return fmt.Errorf("%w: main model %q is not installed; run `llamactl add %s` first",
			ErrUserError, mainID, mainID)
	}
	hw, err := ensureHardware(ctx, d)
	if err != nil {
		return err
	}

	mainModel := models.Model{
		ID: mainMeta.ID, HFRepo: mainMeta.Repo, Arch: mainMeta.Arch,
		ParamsB: mainMeta.ParamsB, MaxCtx: lookupMaxCtx(mainMeta),
	}

	all, err := d.ModelStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list installed models: %w", err)
	}

	var rows []specRow
	for _, candidate := range all {
		if candidate.ID == mainMeta.ID {
			continue // can't draft yourself
		}
		draftModel := models.Model{
			ID: candidate.ID, HFRepo: candidate.Repo, Arch: candidate.Arch,
			ParamsB: candidate.ParamsB, MaxCtx: lookupMaxCtx(candidate),
		}
		verdict := models.SpeculativePair(mainModel, draftModel, hw, "chat")
		if !verdict.ArchMatch {
			continue // omit arch-mismatches entirely (noise, not a candidate)
		}
		v := "ok"
		switch {
		case !verdict.Ok:
			v = "refused"
		case verdict.SizeRatio < models.SpeculativeWarnLowRatio:
			v = "ratio-low"
		case verdict.SizeRatio > models.SpeculativeWarnHighRatio:
			v = "ratio-high"
		}
		rows = append(rows, specRow{
			DraftID:       candidate.ID,
			Arch:          string(candidate.Arch),
			ParamsB:       candidate.ParamsB,
			SizeRatio:     verdict.SizeRatio,
			CombinedRAMGB: verdict.CombinedRAMGB,
			Verdict:       v,
			Reason:        verdict.Reason,
		})
	}

	if len(rows) == 0 {
		fmt.Fprintf(d.Stdout,
			"no installed draft candidates for %s; run `llamactl fit %s` to find smaller variants of the same family\n",
			mainID, mainModel.Arch)
		return nil
	}

	// Sort: Ok rows first (sorted by |SizeRatio - SpeculativeIdealRatio|
	// ascending — closest to the sweet spot rises first), then !Ok rows by
	// Reason.
	sort.SliceStable(rows, func(i, j int) bool {
		ai := rows[i].Verdict == "refused"
		aj := rows[j].Verdict == "refused"
		if ai != aj {
			return !ai
		}
		if !ai {
			di := math.Abs(rows[i].SizeRatio - models.SpeculativeIdealRatio)
			dj := math.Abs(rows[j].SizeRatio - models.SpeculativeIdealRatio)
			return di < dj
		}
		return rows[i].Reason < rows[j].Reason
	})

	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}

	fmt.Fprintf(d.Stdout, "Draft candidates for %s (%g B, %s):\n\n",
		mainID, mainMeta.ParamsB, mainMeta.Arch)
	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DRAFT ID\tARCH\tPARAMSB\tRATIO\tCOMBINED RAM\tVERDICT")
	for _, r := range rows {
		symbol := "✓ ok"
		if r.Verdict == "ratio-low" {
			symbol = "⚠ ratio-low"
		} else if r.Verdict == "ratio-high" {
			symbol = "⚠ ratio-high"
		} else if r.Verdict == "refused" {
			symbol = "✗ refused"
		}
		fmt.Fprintf(tw, "%s\t%s\t%g B\t%.1f×\t%.1f GB\t%s\n",
			r.DraftID, r.Arch, r.ParamsB, r.SizeRatio, r.CombinedRAMGB, symbol)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush tabwriter: %w", err)
	}
	fmt.Fprintln(d.Stdout)
	fmt.Fprintln(d.Stdout, "Note: speculative decoding speedup depends on workload; ratio is a heuristic only.")
	return nil
}
