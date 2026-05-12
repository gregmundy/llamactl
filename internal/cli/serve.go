package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregmundy/llamactl/internal/launchd"
	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/recipes"
	"github.com/gregmundy/llamactl/internal/server"
	"github.com/spf13/cobra"
)

const detachPollInterval = 250 * time.Millisecond
const detachPollDeadline = 5 * time.Second

func newServeCmd(d *Deps) *cobra.Command {
	var port int
	var recipe string
	var detach bool
	cmd := &cobra.Command{
		Use:   "serve <model-id>",
		Short: "Start llama-server for an installed model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), d, args[0], port, recipe, detach)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "TCP port for the OpenAI-compatible endpoint")
	cmd.Flags().StringVar(&recipe, "recipe", recipes.DefaultRecipe, "chat | code | long-context | low-memory")
	cmd.Flags().BoolVar(&detach, "detach", false, "register a launchd LaunchAgent and return")
	return cmd
}

func runServe(ctx context.Context, d *Deps, id string, requestedPort int, recipeName string, detach bool) error {
	meta, err := d.ModelStore.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: model %q is not installed; run `llamactl add %s`", ErrUserError, id, id)
	}

	hw, err := ensureHardware(ctx, d)
	if err != nil {
		return err
	}

	resolution, err := d.ServerResolver.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}
	ver, err := d.ServerProber.Probe(ctx, resolution.Path)
	if err != nil {
		return fmt.Errorf("probe llama-server: %w", err)
	}
	if ver.Build < MinLlamaServerBuild {
		return fmt.Errorf("%w: llama-server at %s (build %d) below minimum supported build %d",
			ErrUserError, resolution.Path, ver.Build, MinLlamaServerBuild)
	}

	caps, err := d.ServerProber.Capabilities(ctx, resolution.Path)
	if err != nil {
		// Capabilities probe failed — log and assume legacy syntax.
		fmt.Fprintf(d.Stderr, "llamactl: warning: capability probe failed (%v); assuming legacy syntax\n", err)
		caps = server.Capabilities{}
	}

	recipe, ok := recipes.Recipes[recipeName]
	if !ok {
		valid := make([]string, 0, len(recipes.Recipes))
		for k := range recipes.Recipes {
			valid = append(valid, k)
		}
		return fmt.Errorf("%w: unknown recipe %q (valid: %s)", ErrUserError, recipeName, strings.Join(valid, ", "))
	}

	model := models.Model{
		ID: meta.ID, HFRepo: meta.Repo, Arch: meta.Arch,
		ParamsB: meta.ParamsB, MaxCtx: lookupMaxCtx(meta),
	}
	sizeGB := float64(meta.SizeBytes) / (1 << 30)
	chosen, err := d.PortAllocator.Free(requestedPort)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}
	switch {
	case requestedPort == 0:
		fmt.Fprintf(d.Stderr, "bound to ephemeral :%d\n", chosen)
	case chosen != requestedPort:
		fmt.Fprintf(d.Stderr, "bound to :%d (:%d was in use)\n", chosen, requestedPort)
	}

	argv := recipes.FlagsFor(recipe, model, meta.Quant, meta.GGUFPath, hw, ver, caps, sizeGB, chosen)

	// Update metadata.LastServedAt before launching. If launch fails the
	// timestamp is slightly inaccurate; acceptable for v1.
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	meta.LastServedAt = now()
	if err := d.ModelStore.Put(ctx, meta); err != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: could not persist LastServedAt: %v\n", err)
	}

	if detach {
		return runServeDetached(ctx, d, meta.ID, resolution.Path, argv, chosen, recipe.Name)
	}
	return runServeForeground(ctx, d, meta.ID, resolution.Path, argv, chosen, recipe.Name)
}

// lookupMaxCtx returns Model.MaxCtx if the model is in PreferredIDs,
// else falls back to 0 (which recipes.FlagsFor treats as "no cap").
// We don't put MaxCtx into Metadata, so this lookup is best-effort.
func lookupMaxCtx(meta models.Metadata) int {
	if m, ok := models.PreferredIDs[meta.ID]; ok {
		return m.MaxCtx
	}
	return 0
}

func runServeForeground(ctx context.Context, d *Deps, id, llamaServer string, argv []string, port int, recipeName string) error {
	if err := os.MkdirAll(d.LogsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}
	logPath := filepath.Join(d.LogsDir, id+".log")
	if _, err := RotateIfLarge(logPath, 10<<20, 3); err != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: log rotation failed: %v\n", err)
		// Continue: serving > rotation hygiene.
	}
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	defer logFile.Close()

	fmt.Fprintf(d.Stdout, "starting llama-server (recipe=%s, port=%d)…\n", recipeName, port)

	cmd := exec.CommandContext(ctx, llamaServer, argv...)
	// Override the default Cancel (SIGKILL) with SIGTERM + 5s grace.
	// llama-server flushes Metal state on SIGTERM in well under that.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdout = io.MultiWriter(logFile, d.Stdout)
	cmd.Stderr = io.MultiWriter(logFile, d.Stderr)
	return cmd.Run()
}

func runServeDetached(ctx context.Context, d *Deps, id, llamaServer string, argv []string, port int, recipeName string) error {
	label := "com.llamactl." + id
	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	if err := os.MkdirAll(d.LogsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir Logs: %w", err)
	}
	logPath := filepath.Join(d.LogsDir, id+".log")
	if _, err := RotateIfLarge(logPath, 10<<20, 3); err != nil {
		fmt.Fprintf(d.Stderr, "llamactl: warning: log rotation failed: %v\n", err)
		// Continue: serving > rotation hygiene.
	}
	plistPath := filepath.Join(d.LaunchAgentsDir, label+".plist")

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	spec := launchd.PlistSpec{
		Label:       label,
		LlamaServer: llamaServer,
		Args:        argv,
		LogPath:     logPath,
		WorkingDir:  home,
	}
	body, err := launchd.Render(spec)
	if err != nil {
		return fmt.Errorf("render plist: %w", err)
	}
	tmp := plistPath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write plist tmp: %w", err)
	}
	if err := os.Rename(tmp, plistPath); err != nil {
		return fmt.Errorf("rename plist: %w", err)
	}

	// If an old instance is loaded, bootout first.
	if existing, _ := d.LaunchdService.Print(ctx, label); existing.PID != 0 {
		_ = d.LaunchdService.Bootout(ctx, label)
	}

	if err := d.LaunchdService.Load(ctx, plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w", err)
	}

	// Poll for PID up to detachPollDeadline.
	deadline := time.Now().Add(detachPollDeadline)
	if d.Now != nil {
		deadline = d.Now().Add(detachPollDeadline)
	}
	for {
		info, _ := d.LaunchdService.Print(ctx, label)
		if info.PID > 0 {
			fmt.Fprintf(d.Stdout, "service %s started (pid=%d, recipe=%s); endpoint http://localhost:%d\n",
				id, info.PID, recipeName, port)
			return nil
		}
		var nowT time.Time
		if d.Now != nil {
			nowT = d.Now()
		} else {
			nowT = time.Now()
		}
		if nowT.After(deadline) {
			return fmt.Errorf("%w: service didn't start within %s; see %s",
				ErrUserError, detachPollDeadline, logPath)
		}
		// select-on-ctx-or-timer instead of bare time.Sleep so SIGINT
		// (and any other ctx cancellation) breaks the poll immediately.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(detachPollInterval):
		}
	}
}
