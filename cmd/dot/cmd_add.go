package main

import "github.com/spf13/cobra"

func init() { register(newAddCmd) }

// newAddCmd only lists `dot add` in the help: it runs what the fallback would, a dot-add
// extension if any, else git add on the profile clone.
func newAddCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:                "add <path>...",
		Short:              "Stage files in the profile clone (git add)",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runExternal(env, append([]string{"add"}, args...))
		},
	}
}
