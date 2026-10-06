package main

import (
	"github.com/fmatsos/dot/internal/selfupdate"
	"github.com/spf13/cobra"
)

func init() { register(newSelfUpdateCmd) }

func newSelfUpdateCmd(env *Env) *cobra.Command {
	var dry bool
	var target string
	cmd := &cobra.Command{
		Use:   "self-update [--version vX.Y.Z] [-n]",
		Short: "Met dot à jour depuis une release GitHub signée",
		Long: "Télécharge la release (la dernière, ou --version vX.Y.Z), vérifie la signature Ed25519 de\n" +
			"SHA256SUMS avec la clé compilée dans le binaire, puis le sha256 du binaire, et remplace\n" +
			"l'exécutable en cours de façon atomique. Toute vérification qui échoue abandonne sans rien\n" +
			"modifier. Sans clé de signature dans le binaire, la mise à jour est refusée.\n" +
			"-n : affiche ce qui serait fait, sans rien télécharger ni écrire.\n\n" +
			"$DOT_RELEASE_BASE et $DOT_RELEASE_API remplacent les URL des releases, pour les tests uniquement.",
		Args: exactArgs(0, "dot self-update [--version vX.Y.Z] [-n]"),
		RunE: func(*cobra.Command, []string) error {
			return selfupdate.Run(selfupdate.Options{
				Current: version, Target: target, Dry: dry, Out: env.Stdout,
				BaseURL: env.Getenv("DOT_RELEASE_BASE"), APIURL: env.Getenv("DOT_RELEASE_API"),
			})
		},
	}
	cmd.Flags().StringVar(&target, "version", "", "version à installer, vX.Y.Z (défaut : la dernière release)")
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "affiche ce qui serait fait sans rien écrire")
	return cmd
}
