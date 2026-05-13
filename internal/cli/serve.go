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
	"github.com/gregmundy/llamactl/internal/platform"
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
	var draftID string // NEW
	cmd := &cobra.Command{
		Use:   "serve <model-id>",
		Short: "Start llama-server for an installed model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), d, args[0], port, recipe, detach, draftID)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "TCP port for the OpenAI-compatible endpoint")
	cmd.Flags().StringVar(&recipe, "recipe", recipes.DefaultRecipe, "chat | code | long-context | low-memory | agent")
	cmd.Flags().BoolVar(&detach, "detach", false, "register a launchd LaunchAgent and return")
	cmd.Flags().StringVar(&draftID, "draft", "", "draft model id for speculative decoding (must be installed)")
	return cmd
}

func runServe(ctx context.Context, d *Deps, id string, requestedPort int, recipeName string, detach bool, draftID string) error {
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

	// Phase 6b: speculative decoding via --draft.
	var verdict models.PairVerdict
	var draftMeta models.Metadata
	hasDraft := draftID != ""
	if hasDraft {
		var err error
		draftMeta, err = d.ModelStore.Get(ctx, draftID)
		if err != nil {
			return fmt.Errorf("%w: draft model %q is not installed; run `llamactl add %s` first",
				ErrUserError, draftID, draftID)
		}
		draftModel := models.Model{
			ID: draftMeta.ID, HFRepo: draftMeta.Repo, Arch: draftMeta.Arch,
			ParamsB: draftMeta.ParamsB, MaxCtx: lookupMaxCtx(draftMeta),
		}
		verdict = models.SpeculativePair(model, draftModel, hw, recipeName)
		if !verdict.Ok {
			return fmt.Errorf("%w: %s", ErrUserError, verdict.Reason)
		}
		if verdict.Reason != "" {
			// Warning band (Ok=true but Reason non-empty): print to stderr, continue.
			fmt.Fprintf(d.Stderr, "llamactl: warning: %s\n", verdict.Reason)
		}
	}

	// Build the skip list of ports already claimed by sibling
	// com.llamactl.* services. Without this, two `serve --detach`
	// invocations on different models within a few seconds can both
	// pick the same port: the first service's child llama-server
	// hasn't called bind() yet (it's still loading the model into RAM)
	// so net.Listen sees the port as free.
	//
	// If we're re-serving an existing service for this same model id,
	// drop our own port from skip so we keep using it.
	skipPorts, _ := launchd.PortsInUse(d.LaunchAgentsDir)
	ownLabel := "com.llamactl." + meta.ID
	if ownPort := launchd.PortFor(d.LaunchAgentsDir, ownLabel); ownPort > 0 {
		filtered := skipPorts[:0]
		for _, p := range skipPorts {
			if p != ownPort {
				filtered = append(filtered, p)
			}
		}
		skipPorts = filtered
	}
	chosen, err := d.PortAllocator.Free(requestedPort, skipPorts)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUserError, err)
	}
	switch {
	case requestedPort == 0:
		fmt.Fprintf(d.Stderr, "bound to ephemeral :%d\n", chosen)
	case chosen != requestedPort:
		fmt.Fprintf(d.Stderr, "bound to :%d (:%d was in use)\n", chosen, requestedPort)
	}

	argv := recipes.FlagsFor(recipe, model, meta.Quant, meta.GGUFPath, hw, ver, caps, sizeGB, chosen, platform.Default{}.Cores())

	// Resolve API key: env var takes precedence over config file.
	apiKey := ""
	if d.Getenv != nil {
		apiKey = d.Getenv("LLAMACTL_API_KEY")
	}
	if apiKey == "" && d.Config != nil {
		apiKey = d.Config.APIKey
	}
	if apiKey != "" {
		argv = append(argv, "--api-key", apiKey)
	}

	// Phase 6b: append speculative-decoding args when --draft was set + validated.
	if hasDraft {
		// Draft ctx capped at min(main ctx, draft.MaxCtx). model.MaxCtx is the
		// main model's training ctx; 0 means "no cap known" — fall back to a
		// generic 8192 (matching the chat recipe's default).
		mainCtx := model.MaxCtx
		if mainCtx == 0 {
			mainCtx = 8192
		}
		draftCtx := lookupMaxCtx(draftMeta)
		if draftCtx == 0 || draftCtx > mainCtx {
			draftCtx = mainCtx
		}
		argv = append(argv, "--model-draft", draftMeta.GGUFPath)
		argv = append(argv, "--ctx-size-draft", fmt.Sprintf("%d", draftCtx))
	}

	if hasDraft {
		fmt.Fprintf(d.Stdout, "speculative decoding enabled (draft=%s, ratio=%.1f×)\n",
			draftMeta.ID, verdict.SizeRatio)
	}

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

	userHomeDir := os.UserHomeDir
	if d.UserHomeDir != nil {
		userHomeDir = d.UserHomeDir
	}
	home, err := userHomeDir()
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
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	sleep := time.After
	if d.Sleep != nil {
		sleep = d.Sleep
	}
	deadline := now().Add(detachPollDeadline)
	for {
		info, _ := d.LaunchdService.Print(ctx, label)
		if info.PID > 0 {
			fmt.Fprintf(d.Stdout, "service %s started (pid=%d, recipe=%s); endpoint http://localhost:%d\n",
				id, info.PID, recipeName, port)
			return nil
		}
		if now().After(deadline) {
			return fmt.Errorf("%w: service didn't start within %s; see %s",
				ErrUserError, detachPollDeadline, logPath)
		}
		// select-on-ctx-or-timer instead of bare time.Sleep so SIGINT
		// (and any other ctx cancellation) breaks the poll immediately.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sleep(detachPollInterval):
		}
	}
}
