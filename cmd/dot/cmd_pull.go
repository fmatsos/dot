package main

import "github.com/spf13/cobra"

func init() { register(newPullCmd) }

func newPullCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Fetch the profiles' changes (git pull --rebase), then reinstall them",
		Long: "For each profile: git pull --rebase. Then install the updated profiles (links, tools, modules).\n" +
			"Without -p, $DOT_PROFILE or $DOTFILES_DEPLOY, every registered profile is processed; a failing profile\n" +
			"does not stop the others, the exit code is 1 if there is one.",
		Args: exactArgs(0, "dot pull [-p <key>]"),
		RunE: func(*cobra.Command, []string) error {
			batch, err := targetProfiles(env)
			if err != nil {
				return err
			}
			return installErr(newInstaller(env, false).Pull(batch))
		},
	}
}
