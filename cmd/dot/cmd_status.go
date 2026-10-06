package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() { register(newStatusCmd) }

// driftReport lists the home/ files of a profile clone that are no longer links; nil until
// internal/link provides it. Its lines go to stderr after the git status.
var driftReport func(dir string) []string

func newStatusCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   "Changements des clones de profils, et fichiers de home/ qui ne sont plus des liens",
		Long: "Pour chaque profil : état du clone (git status -sb : changements, branche, avance et retard), puis les\n" +
			"fichiers de home/ qui ne sont plus des liens.\n" +
			"Sans -p, $DOT_PROFILE ni $DOTFILES_DEPLOY, tous les profils inscrits sont listés, chacun sous une ligne\n" +
			"« ==> <clé> ». C'est un rapport : le code de sortie est 0.\n\n" +
			"Un fichier « détaché » a été réécrit par un outil au lieu d'être modifié à travers le lien : " +
			"ses changements n'iront jamais dans le dépôt. Remets le lien (dot install) après avoir reporté le contenu.",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error {
			batch, err := targetProfiles(env)
			if err != nil {
				return err
			}
			for _, p := range batch {
				if len(batch) > 1 {
					fmt.Fprintf(env.Stdout, "==> %s\n", p.Key)
				}
				// git reports its own failure (missing clone…) on stderr; a report goes on to the next profile.
				if err := runWithStdio(env, gitCmd(p.Dir, "status", "-sb")); err != nil {
					if _, ok := err.(exitError); !ok {
						fmt.Fprintf(env.Stderr, "status %s : %v\n", p.Key, err)
					}
					continue
				}
				if driftReport != nil {
					for _, l := range driftReport(p.Dir) {
						fmt.Fprintln(env.Stderr, l)
					}
				}
			}
			return nil
		},
	}
}
