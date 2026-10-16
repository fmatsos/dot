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
		Use:   "install [url] [-p <key>] [-n]",
		Short: "Clone a profile into ~/.dot/<key>, register and install it (without url: reinstall)",
		Long: "With a url: partial clone into ~/.dot/<key> (key: -p, else the repository name), registration in the registry\n" +
			"(the first profile becomes the default), then installation. Without url: reinstall the registered profiles.\n\n" +
			"Installation: git identity of the clone, links from home/ into ~ and from bin/ into ~/.local/bin (existing\n" +
			"files go to ~/.local/state/dotfiles/backup/<date>/), backup purge, tools, manifest\n" +
			"modules. Two profiles linking the same file: refused before any write. Idempotent.\n\n" +
			"$DOTFILES_TOOLS=0 skips the tools (mise, nvm, Codex plugins); $DOTFILES_BACKUP_DAYS sets the backup\n" +
			"retention (default 30, 0 keeps everything). -n writes nothing: it shows what would change.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usagef("usage: dot install [<url>] [-p <key>] [-n]")
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
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show what would change without writing anything")
	return cmd
}
