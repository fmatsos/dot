package main

import "github.com/spf13/cobra"

func init() { register(newUninstallCmd) }

const uninstallUsage = "usage : dot uninstall -p <clé> [--purge]"

func newUninstallCmd(env *Env) *cobra.Command {
	var purge, force bool
	cmd := &cobra.Command{
		Use:   "uninstall -p <clé> [--purge]",
		Short: "Retire les liens d'un profil et son entrée du registre (le clone est gardé)",
		Long: "Retire les liens du profil dans ~, puis son entrée du registre. Le clone ~/.dot/<clé> est gardé sauf avec\n" +
			"--purge, qui le supprime (refusé s'il a des changements non commités, sauf --force). Le profil par défaut\n" +
			"ne peut partir que s'il est le dernier.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 || env.Profile == "" {
				return usagef(uninstallUsage)
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			return newInstaller(env, false).Uninstall(env.Profile, purge, force)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "supprime aussi le clone")
	cmd.Flags().BoolVar(&force, "force", false, "avec --purge : supprime même avec des changements non commités")
	return cmd
}
