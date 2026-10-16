package main

import (
	"path/filepath"

	"github.com/fmatsos/dot/internal/skills"
	"github.com/spf13/cobra"
)

func init() { aiCommands = append(aiCommands, newSkillsCmd) }

// skillsList is a profile's versioned list of skills.
func skillsList(dir string) string {
	return filepath.Join(dir, "home", ".config", "skills", "skills.json")
}

func newSkillsCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "skills [-n]",
		Short: "Install the agent skills the profiles list, through npx " + skills.CLI,
		Long: "Apply <profile>/home/.config/skills/skills.json, {\"<source>[#ref]\": [\"<skill>\", ...]}, to the\n" +
			"global skills of the coding agents (-n: preview). The list is authoritative: a skill installed\n" +
			"from a source and missing from it is removed; a skill without a source (copied by hand) is left\n" +
			"alone and reported as unmanaged. Without -p, every registered profile is merged in registry order.\n\n" +
			"~/.config/skills/skills.local.json (optional): a local source replaces the profiles' one; null\n" +
			"removes it. The skills CLI runs pinned (" + skills.CLI + "), through npx.",
		Args: exactArgs(0, "dot ai skills [-n]"),
		RunE: func(*cobra.Command, []string) error {
			dirs, _, err := targetProfileDirs(env)
			if err != nil {
				return reportErr(env, "skills", err)
			}
			o := skills.Options{Home: env.Home, DryRun: dry, Out: env.Stdout}
			for _, d := range dirs {
				o.Sources = append(o.Sources, skillsList(d))
			}
			if err := skills.Run(o); err != nil {
				return reportErr(env, "skills", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show the plan without installing or removing anything")
	return cmd
}
