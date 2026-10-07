package main

import (
	"github.com/spf13/cobra"
)

// completeProfileKeys returns the registered profile keys for shell completion.
// On any error, it returns no completions with NoFileComp directive.
func completeProfileKeys(env *Env) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		r, err := env.Registry()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return r.Keys(), cobra.ShellCompDirectiveNoFileComp
	}
}

// completeConfigKeys returns the valid keys for `dot config get|set|unset`.
// Returns: default, profiles.<key>.repo for each registered profile.
// On any error, it returns no completions with NoFileComp directive.
func completeConfigKeys(env *Env) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		r, err := env.Registry()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		keys := []string{"default"}
		for _, key := range r.Keys() {
			keys = append(keys, "profiles."+key+".repo")
		}
		return keys, cobra.ShellCompDirectiveNoFileComp
	}
}
