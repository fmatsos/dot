package main

import (
	"path/filepath"

	"github.com/fmatsos/dot/internal/doctor"
	"github.com/spf13/cobra"
)

func init() { register(newDoctorCmd) }

func newDoctorCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Bilan local des dotfiles, sans écriture ni fetch",
		Long: "dot doctor — bilan local des dotfiles, sans écriture, fetch ni saisie interactive.\n\n" +
			"Vérifie déploiement, liens, garde-fou, outils, secrets, plugins et identités git :\n" +
			"✓ sain, ! avertissement avec piste de correction, ✗ échec (code 1).\n" +
			"L'avance et le retard git viennent du dernier fetch. Aucune valeur de secret affichée.\n" +
			"Sans -p, tous les profils inscrits sont vérifiés dans l'ordre du registre, chacun sous\n" +
			"une ligne « profil <clé> » ; les vérifications de la machine ne passent qu'une fois.",
		DisableFlagsInUseLine: true,
		Args:                  exactArgs(0, "dot doctor"),
		RunE: func(*cobra.Command, []string) error {
			dirs, multi, err := targetProfileDirs(env)
			o := doctor.Options{
				Home: env.Home, Err: err, Headers: multi,
				SrcRoot: srcRoot(env), SandboxRoot: sandboxRoot(env),
			}
			for _, d := range dirs {
				if err != nil {
					break // an unresolved target leaves nothing to check but the machine
				}
				o.Profiles = append(o.Profiles, doctor.Profile{Key: filepath.Base(d), Dir: d})
			}
			lines := doctor.Run(o)
			if err := doctor.Write(env.Stdout, lines, useColor(env, env.Stdout)); err != nil {
				return err
			}
			if doctor.HasFail(lines) {
				return exitError{code: 1}
			}
			return nil
		},
	}
}
