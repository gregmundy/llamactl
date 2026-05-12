package cli

import "github.com/spf13/cobra"

// NewRoot returns the root cobra command. Pass the llamactl version string
// (e.g. "v0.1.0") so `--version` can report it.
func NewRoot(deps *Deps, llamactlVersion string) *cobra.Command {
	root := &cobra.Command{
		Use:           "llamactl",
		Short:         "Run llama.cpp on Apple Silicon",
		Version:       llamactlVersion,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	if deps.Stdout != nil {
		root.SetOut(deps.Stdout)
	}
	if deps.Stderr != nil {
		root.SetErr(deps.Stderr)
	}
	root.AddCommand(newHardwareCmd(deps))
	root.AddCommand(newDoctorCmd(deps))
	root.AddCommand(newAddCmd(deps))
	root.AddCommand(newFitCmd(deps))
	root.AddCommand(newSearchCmd(deps))
	root.AddCommand(newListCmd(deps))
	root.AddCommand(newRemoveCmd(deps))
	root.AddCommand(newServeCmd(deps))
	root.AddCommand(newStopCmd(deps))
	root.AddCommand(newStatusCmd(deps))
	root.AddCommand(newCacheCmd(deps))
	root.AddCommand(newConfigCmd(deps))

	// Cobra's SilenceUsage on the root does not auto-propagate; failing
	// subcommands would print usage to stdout unless each child also has it
	// set. Walk the entire command tree and set it explicitly.
	silenceUsageTree(root)

	return root
}

// silenceUsageTree recursively sets SilenceUsage=true on every descendant of
// cmd so that usage is never printed on error regardless of how a subcommand
// is invoked.
func silenceUsageTree(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		child.SilenceUsage = true
		silenceUsageTree(child)
	}
}
