package main

import "github.com/spf13/cobra"

func init() { register(newPullCmd) }

func newPullCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Récupère les changements des profils (git pull --rebase), puis les réinstalle",
		Long: "Pour chaque profil : git pull --rebase. Puis installation des profils à jour (liens, outils, modules).\n" +
			"Sans -p, $DOT_PROFILE ni $DOTFILES_DEPLOY, tous les profils inscrits sont traités ; un profil en échec\n" +
			"n'arrête pas les autres, le code de sortie est 1 s'il y en a un.",
		Args: exactArgs(0, "dot pull [-p <clé>]"),
		RunE: func(*cobra.Command, []string) error {
			batch, err := targetProfiles(env)
			if err != nil {
				return err
			}
			return installErr(newInstaller(env, false).Pull(batch))
		},
	}
}
