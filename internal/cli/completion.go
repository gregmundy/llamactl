package cli

import (
	"sort"
	"strings"

	"github.com/gregmundy/llamactl/internal/models"
	"github.com/gregmundy/llamactl/internal/recipes"
	"github.com/spf13/cobra"
)

// Cobra ValidArgsFunctions and flag-completion helpers. Registered against
// the existing serve/stop/remove/add/config/fit commands by their newXxxCmd
// constructors. Cobra calls these during the user's tab-completion request
// (zsh/bash/fish/powershell) — the framework handles the actual shell wire
// protocol via the `llamactl completion <shell>` subcommand.
//
// All completers return ShellCompDirectiveNoFileComp so the shell doesn't
// fall back to filename completion when our list is empty (which would be
// the wrong UX for "complete an installed model id").

// completeInstalledModels returns the names of every model present in the
// ModelStore. The optional `exclude` argument filters out a value the
// caller already chose (e.g. `serve --draft <X>` shouldn't suggest the
// same id that's already the main positional).
func completeInstalledModels(d *Deps, exclude ...string) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	skip := make(map[string]bool, len(exclude))
	for _, s := range exclude {
		if s != "" {
			skip[s] = true
		}
	}
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if d.ModelStore == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		entries, err := d.ModelStore.List(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(entries))
		for _, m := range entries {
			if skip[m.ID] {
				continue
			}
			out = append(out, m.ID)
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeRunningServiceNames returns the run names of detached services
// currently registered with launchd. Used for `stop <run-name>`.
//
// Reads the in-process Deps.LaunchdService.List — same source `status`
// uses, so the completion mirrors what `status` would show.
func completeRunningServiceNames(d *Deps) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if d.LaunchdService == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		svcs, err := d.LaunchdService.List(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(svcs))
		for _, s := range svcs {
			out = append(out, strings.TrimPrefix(s.Label, "com.llamactl."))
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completePreferredOrModelPath suggests preferred-id short names. Skips
// suggestions entirely once the user has typed a `/` — the HF-path mode
// can't be enumerated cheaply (would need an API call per keystroke) so
// we let them type free-form.
func completePreferredOrModelPath() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if strings.Contains(toComplete, "/") {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(models.PreferredIDs))
		for id := range models.PreferredIDs {
			out = append(out, id)
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeRecipeNames lists the recipes registered in recipes.Recipes —
// includes any future entry without needing to keep this helper in sync.
func completeRecipeNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	out := make([]string, 0, len(recipes.Recipes))
	for name := range recipes.Recipes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

// configKeys is the static set `config get/set` accepts. Hand-maintained
// to match the yaml tags on config.Config — that struct rarely changes
// and a drift here would be caught by the unknown-key path in runConfigSet.
var configKeys = []string{
	"api_key",
	"default_port",
	"hf_token",
	"llama_server_path",
	"log_level",
	"models_dir",
}

func completeConfigKeys(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return configKeys, cobra.ShellCompDirectiveNoFileComp
}
