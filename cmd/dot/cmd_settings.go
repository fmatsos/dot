package main

import (
	"fmt"
	"path/filepath"

	"github.com/fmatsos/dot/internal/settings"
	"github.com/spf13/cobra"
)

func init() { register(newSettingsCmd) }

// targetProfileDirs lists the profile clones a shared-file command acts on: exactly one when
// DOTFILES_DEPLOY, -p or DOT_PROFILE names it, else every registered profile in registry order.
func targetProfileDirs(env *Env) (dirs []string, multi bool, err error) {
	if env.Getenv("DOTFILES_DEPLOY") != "" || env.Profile != "" || env.Getenv("DOT_PROFILE") != "" {
		d, err := env.ProfileDir()
		return []string{d}, false, err
	}
	keys, err := env.ProfileKeys()
	if err != nil {
		return nil, false, err
	}
	if len(keys) == 0 {
		_, err := env.ProfileDir() // reports the empty registry the usual way
		return nil, false, err
	}
	for _, k := range keys {
		dirs = append(dirs, env.ProfileDirFor(k))
	}
	return dirs, len(dirs) > 1, nil
}

// reportErr prints "<prefix>: <err>" like the bash scripts did and returns a silent exit 1.
func reportErr(env *Env, prefix string, err error) error {
	fmt.Fprintf(env.Stderr, "%s: %v\n", prefix, err)
	return exitError{code: 1}
}

func newSettingsCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "settings [-n]",
		Short: "Merge the Claude settings with the versioned base (-n for the diff only)",
		Long: "Merge <profile>/home/.claude/settings.base.json into ~/.claude/settings.json: the base wins,\n" +
			"extra local keys are kept. Without -p, every registered profile is merged in\n" +
			"registry order. The diff shows no context lines; the old file is backed up.",
		Args: exactArgs(0, "dot settings [-n]"),
		RunE: func(*cobra.Command, []string) error {
			dirs, multi, err := targetProfileDirs(env)
			if err != nil {
				return reportErr(env, "settings", err)
			}
			o := settings.Options{Home: env.Home, SkipMissing: multi, DryRun: dry, Out: env.Stdout}
			for _, d := range dirs {
				o.Bases = append(o.Bases, filepath.Join(d, "home", ".claude", "settings.base.json"))
			}
			if err := settings.Run(o); err != nil {
				return reportErr(env, "settings", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show the diff without writing anything")
	return cmd
}
