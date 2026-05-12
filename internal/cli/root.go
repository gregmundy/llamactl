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
	root.AddCommand(newSearchCmd(deps))
	root.AddCommand(newListCmd(deps))
	root.AddCommand(newRemoveCmd(deps))
	root.AddCommand(newServeCmd(deps))
	root.AddCommand(newStopCmd(deps))
	root.AddCommand(newStatusCmd(deps))
	root.AddCommand(newCacheCmd(deps))
	return root
}
