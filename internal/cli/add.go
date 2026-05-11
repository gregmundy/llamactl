package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregmundy/llamactl/internal/download"
	"github.com/gregmundy/llamactl/internal/hardware"
	"github.com/gregmundy/llamactl/internal/hf"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAddCmd(d *Deps) *cobra.Command {
	var quantOverride string
	var targetCtx int
	cmd := &cobra.Command{
		Use:   "add <model-id>",
		Short: "Download a whitelisted model and write metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd.Context(), d, args[0], quantOverride, targetCtx)
		},
	}
	cmd.Flags().StringVar(&quantOverride, "quant", "", "override automatic quant selection")
	cmd.Flags().IntVar(&targetCtx, "ctx", 8192, "target context size for quant calculation")
	return cmd
}

func runAdd(ctx context.Context, d *Deps, id, quantOverride string, targetCtx int) error {
	model, err := models.LookupOrSuggest(id)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}

	hw, err := ensureHardware(ctx, d)
	if err != nil {
		return err
	}

	var quant models.Quant
	if quantOverride != "" {
		if !isKnownQuant(quantOverride) {
			return fmt.Errorf("%w: unknown --quant %q (known: %s)", ErrUserError, quantOverride, knownQuantsList())
		}
		quant = models.Quant(quantOverride)
	} else {
		quant, err = d.QuantSelector.Select(model, hw, targetCtx)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUserError, err)
		}
	}

	repo, err := d.HFClient.RepoInfo(ctx, model.HFRepo)
	if err != nil {
		return fmt.Errorf("fetch HF repo info: %w", err)
	}
	file, expectedSHA, totalSize, err := findQuantFile(repo, quant)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}

	destDir := filepath.Join(d.SharedModelsDir, model.ID)
	destPath := filepath.Join(destDir, string(quant)+".gguf")

	if existing, _ := sha256OfFileIfExists(destPath); existing == expectedSHA {
		fmt.Fprintf(d.Stdout, "already present (matched SHA): %s\n", destPath)
	} else {
		req := download.Request{
			RepoID:         model.HFRepo,
			File:           file,
			DestPath:       destPath,
			ExpectedSHA256: expectedSHA,
			TotalSize:      totalSize,
			Progress:       newProgress(d, totalSize),
		}
		if err := d.Downloader.Get(ctx, req); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}

	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	meta := models.Metadata{
		ID:        model.ID,
		Repo:      model.HFRepo,
		Quant:     quant,
		SHA256:    expectedSHA,
		GGUFPath:  destPath,
		SizeBytes: totalSize,
		AddedAt:   now(),
	}
	if err := d.ModelStore.Put(ctx, meta); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	fmt.Fprintf(d.Stdout, "installed %s (%s, %s) -> %s\n",
		model.ID, quant, humanFileSize(totalSize), destPath)
	return nil
}

// ensureHardware reads hardware.json if present, else runs the detector and
// persists the result. Mirrors hardware.go's marshal + WriteFile pattern.
func ensureHardware(ctx context.Context, d *Deps) (hardware.Info, error) {
	data, err := os.ReadFile(d.HardwareJSONPath)
	if err == nil {
		var info hardware.Info
		if jerr := json.Unmarshal(data, &info); jerr == nil {
			return info, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return hardware.Info{}, fmt.Errorf("read hardware.json: %w", err)
	}
	info, derr := d.HardwareDetector.Detect(ctx)
	if derr != nil {
		return hardware.Info{}, fmt.Errorf("detect hardware: %w", derr)
	}
	if err := os.MkdirAll(filepath.Dir(d.HardwareJSONPath), 0o755); err != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: mkdir for hardware.json: %v\n", err)
		return info, nil
	}
	b, mErr := json.MarshalIndent(info, "", "  ")
	if mErr != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: marshal hardware.json: %v\n", mErr)
		return info, nil
	}
	if werr := os.WriteFile(d.HardwareJSONPath, b, 0o644); werr != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: persist hardware.json: %v\n", werr)
	}
	return info, nil
}

// findQuantFile looks for a sibling whose filename contains the quant
// (case-insensitive) and is a .gguf file. Rejects multi-shard (-N-of-M).
func findQuantFile(repo hf.Repo, quant models.Quant) (file, sha string, size int64, err error) {
	qLower := strings.ToLower(string(quant))
	available := make([]string, 0, len(repo.Siblings))
	for _, s := range repo.Siblings {
		if !strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
			continue
		}
		available = append(available, s.RFilename)
		if !strings.Contains(strings.ToLower(s.RFilename), qLower) {
			continue
		}
		if strings.Contains(s.RFilename, "-of-") {
			return "", "", 0, fmt.Errorf("multi-shard GGUF (%s) not supported in v1", s.RFilename)
		}
		if s.LFS == nil || s.LFS.SHA256 == "" {
			return "", "", 0, fmt.Errorf("HF sibling %s missing lfs.sha256", s.RFilename)
		}
		return s.RFilename, s.LFS.SHA256, s.LFS.Size, nil
	}
	return "", "", 0, fmt.Errorf("no %s file in %s; available: %s", quant, repo.ID, strings.Join(available, ", "))
}

func isKnownQuant(q string) bool {
	for _, p := range models.PreferenceOrder {
		if string(p) == q {
			return true
		}
	}
	return false
}

func knownQuantsList() string {
	out := make([]string, 0, len(models.PreferenceOrder))
	for _, q := range models.PreferenceOrder {
		out = append(out, string(q))
	}
	return strings.Join(out, ", ")
}

func sha256OfFileIfExists(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func humanFileSize(n int64) string {
	const u = 1024.0
	if n < int64(u) {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := u, 0
	for x := float64(n) / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/div, "KMGTPE"[exp])
}

// newProgress returns a Progress configured for the current stderr. Returns
// nil when stderr is not a TTY OR when totalSize is 0 (unknown).
func newProgress(d *Deps, totalSize int64) *download.Progress {
	if totalSize <= 0 {
		return nil
	}
	f, ok := d.Stderr.(*os.File)
	isTTY := ok && term.IsTerminal(int(f.Fd()))
	if !isTTY {
		return nil
	}
	return &download.Progress{Out: d.Stderr, Total: totalSize, IsTTY: true}
}
