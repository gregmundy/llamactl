package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// bootoutSettleDeadline caps how long runServeDetached waits for
// `launchctl bootout` to actually tear down the prior instance before
// it retries `launchctl bootstrap`. launchctl bootout returns before
// launchd finishes — bootstrap exit-5s if the label is still registered.
const bootoutSettleDeadline = 3 * time.Second

// runNameRe limits --name to a charset that's safe for launchd labels,
// plist filenames, log filenames, and shell display. Same shape model
// ids already use (qwen2.5-0.5b-instruct, llama3.1-8b, etc.). Must
// start with an alphanumeric so labels never begin with a dot or dash.
var runNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validateRunName(name string) error {
	if !runNameRe.MatchString(name) {
		return fmt.Errorf("%w: run name %q invalid (must match %s)",
			ErrUserError, name, runNameRe.String())
	}
	return nil
}

func newServeCmd(d *Deps) *cobra.Command {
	var port int
	var recipe string
	var detach bool
	var draftID string
	var runName string
	cmd := &cobra.Command{
		Use:   "serve <model-id>",
		Short: "Start llama-server for an installed model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default name = model id. Lets single-instance users see no
			// surprise and lets multi-instance users disambiguate
			// (e.g. same model at different recipes / ports).
			name := runName
			if name == "" {
				name = args[0]
			}
			if err := validateRunName(name); err != nil {
				return err
			}
			return runServe(cmd.Context(), d, args[0], name, port, recipe, detach, draftID)
		},
	}
	cmd.Flags().IntVar(&port, "port", 8080, "TCP port for the OpenAI-compatible endpoint")
	cmd.Flags().StringVar(&recipe, "recipe", recipes.DefaultRecipe, "chat | code | long-context | low-memory | agent")
	cmd.Flags().BoolVar(&detach, "detach", false, "register a launchd LaunchAgent and return")
	cmd.Flags().StringVar(&draftID, "draft", "", "draft model id for speculative decoding (must be installed)")
	cmd.Flags().StringVar(&runName, "name", "", "run name (defaults to <model-id>; lets you run the same model multiple times in parallel)")

	// Tab completion: positional is an installed model id; --recipe is a
	// small static set; --draft is also an installed id but filters out
	// whatever the positional already chose (a model can't draft itself).
	cmd.ValidArgsFunction = completeInstalledModels(d)
	_ = cmd.RegisterFlagCompletionFunc("recipe", completeRecipeNames)
	_ = cmd.RegisterFlagCompletionFunc("draft", func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var mainID string
		if len(args) > 0 {
			mainID = args[0]
		}
		return completeInstalledModels(d, mainID)(c, args, toComplete)
	})
	return cmd
}

func runServe(ctx context.Context, d *Deps, id, name string, requestedPort int, recipeName string, detach bool, draftID string) error {
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
	// If we're re-serving an existing run with the same name (the
	// default-named "atomic replace" path, or an explicit --name
	// re-serve), drop our own port from skip so we keep using it.
	skipPorts, _ := launchd.PortsInUse(d.LaunchAgentsDir)
	ownLabel := "com.llamactl." + name
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
		return runServeDetached(ctx, d, name, resolution.Path, argv, chosen, recipe.Name)
	}
	return runServeForeground(ctx, d, name, resolution.Path, argv, chosen, recipe.Name)
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

func runServeForeground(ctx context.Context, d *Deps, name, llamaServer string, argv []string, port int, recipeName string) error {
	if err := os.MkdirAll(d.LogsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}
	logPath := filepath.Join(d.LogsDir, name+".log")
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

func runServeDetached(ctx context.Context, d *Deps, name, llamaServer string, argv []string, port int, recipeName string) error {
	label := "com.llamactl." + name
	if err := os.MkdirAll(d.LaunchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir LaunchAgents: %w", err)
	}
	if err := os.MkdirAll(d.LogsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir Logs: %w", err)
	}
	logPath := filepath.Join(d.LogsDir, name+".log")
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

	// If an old instance is loaded, bootout AND wait for launchd to
	// actually finish teardown before bootstrap. launchctl bootout
	// returns asynchronously — bootstrap-immediately-after exit-5s
	// because the label is still registered. Pre-v1.5.0 this bug
	// surfaced as "second `serve` for the same name fails."
	if existing, _ := d.LaunchdService.Print(ctx, label); existing.PID != 0 {
		_ = d.LaunchdService.Bootout(ctx, label)
		if err := waitForLabelGone(ctx, d, label); err != nil {
			return fmt.Errorf("bootout existing %s: %w", label, err)
		}
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
				name, info.PID, recipeName, port)
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

// waitForLabelGone polls launchctl print until PID==0 (label torn down)
// or bootoutSettleDeadline elapses. launchctl bootout returns before
// launchd actually unloads the job; without this wait, an immediate
// bootstrap of the same label exit-5s on macOS.
//
// Returns nil on successful teardown (or if the label was never up),
// context error on ctx cancel, or a deadline error if launchd hasn't
// torn down within the cap — in which case the caller's bootstrap will
// likely also fail, but with a clearer error surface than a silent
// timeout deeper in the flow.
func waitForLabelGone(ctx context.Context, d *Deps, label string) error {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	sleep := time.After
	if d.Sleep != nil {
		sleep = d.Sleep
	}
	deadline := now().Add(bootoutSettleDeadline)
	for {
		info, _ := d.LaunchdService.Print(ctx, label)
		if info.PID == 0 {
			return nil
		}
		if now().After(deadline) {
			return fmt.Errorf("launchctl still reports pid=%d after %s",
				info.PID, bootoutSettleDeadline)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sleep(detachPollInterval):
		}
	}
}
