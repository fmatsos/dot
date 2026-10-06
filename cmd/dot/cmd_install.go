package main

import (
	"errors"
	"path/filepath"

	"github.com/fmatsos/dot/internal/install"
	"github.com/spf13/cobra"
)

func init() { register(newInstallCmd) }

// installToolsRequest is what the tools step (mise, nvm, plugins) receives.
type installToolsRequest = install.ToolsRequest

// runTools is the hook to the tools step; nil does nothing. It runs once per install, after the
// links, with the profiles of the run (Skip is $DOTFILES_TOOLS=0, Dry the -n flag).
var runTools func(env *Env, req installToolsRequest) error

// newInstaller builds the installer of a run from the environment.
func newInstaller(env *Env, dry bool) *install.Installer {
	in := install.New(env.Home, dry, env.Stdout, env.Stderr)
	in.Getenv = env.Getenv
	in.Tools = func(req install.ToolsRequest) error {
		if runTools == nil {
			return nil
		}
		return runTools(env, req)
	}
	return in
}

// installErr turns an already printed failure into a silent exit 1.
func installErr(err error) error {
	if errors.Is(err, install.ErrReported) {
		return exitError{code: 1}
	}
	return err
}

// targetProfiles lists the profiles a command acts on (see targetProfileDirs).
func targetProfiles(env *Env) ([]install.Profile, error) {
	dirs, _, err := targetProfileDirs(env)
	if err != nil {
		return nil, err
	}
	ps := make([]install.Profile, len(dirs))
	for i, d := range dirs {
		ps[i] = install.Profile{Key: filepath.Base(d), Dir: d}
	}
	return ps, nil
}

func newInstallCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "install [url] [-p <clé>] [-n]",
		Short: "Clone un profil dans ~/.dot/<clé>, l'inscrit et l'installe (sans url : réinstalle)",
		Long: "Avec une url : clone partiel dans ~/.dot/<clé> (clé : -p, sinon le nom du dépôt), inscription au registre\n" +
			"(le premier profil devient le défaut), puis installation. Sans url : réinstalle les profils inscrits.\n\n" +
			"Installation : identité git du clone, liens de home/ dans ~ et de bin/ dans ~/.local/bin (les fichiers\n" +
			"existants vont dans ~/.local/state/dotfiles/backup/<date>/), purge des sauvegardes, outils, modules du\n" +
			"manifeste. Deux profils qui lient le même fichier : refus avant toute écriture. Idempotent.\n\n" +
			"$DOTFILES_TOOLS=0 saute les outils ; $DOTFILES_BACKUP_DAYS règle la rétention des sauvegardes (défaut 30,\n" +
			"0 garde tout). -n n'écrit rien : il affiche ce qui changerait.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usagef("usage : dot install [<url>] [-p <clé>] [-n]")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			in := newInstaller(env, dry)
			if len(args) == 1 {
				key := env.Profile
				if key == "" {
					var err error
					if key, err = install.DeriveKey(args[0]); err != nil {
						return err
					}
				}
				return installErr(in.Add(args[0], key))
			}
			batch, err := targetProfiles(env)
			if err != nil {
				return err
			}
			return installErr(in.Reinstall(batch))
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "affiche ce qui changerait sans rien écrire")
	return cmd
}
