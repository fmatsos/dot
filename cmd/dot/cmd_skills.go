package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

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
			return applySkills(env, dirs, dry)
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show the plan without installing or removing anything")
	cmd.AddCommand(newSkillsAdoptCmd(env), newSkillsAddCmd(env), newSkillsRmCmd(env),
		newSkillsPassCmd(env, "update", "Update the installed global skills (npx skills update -g -y)", "update", "-g", "-y"),
		newSkillsPassCmd(env, "find", "Search the skills of skills.sh (npx skills find)", "find"))
	return cmd
}

func applySkills(env *Env, dirs []string, dry bool) error {
	o := skills.Options{Home: env.Home, DryRun: dry, Out: env.Stdout}
	for _, d := range dirs {
		o.Sources = append(o.Sources, skillsList(d))
	}
	if err := skills.Run(o); err != nil {
		return reportErr(env, "skills", err)
	}
	return nil
}

// skillsProfile is the one profile whose list an edit writes: -p is needed with several.
func skillsProfile(env *Env) (string, error) {
	dirs, multi, err := targetProfileDirs(env)
	if err != nil {
		return "", err
	}
	if multi {
		return "", usagef("several profiles registered: specify -p <key>")
	}
	return dirs[0], nil
}

// allProfileDirs are the lists an edit is applied with: every profile, even under -p, since a
// list applied alone would remove the skills of the others.
func allProfileDirs(env *Env) ([]string, error) {
	if env.Getenv("DOTFILES_DEPLOY") != "" {
		d, err := env.ProfileDir()
		return []string{d}, err
	}
	keys, err := env.ProfileKeys()
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, k := range keys {
		dirs = append(dirs, env.ProfileDirFor(k))
	}
	return dirs, nil
}

// editSkills changes the target profile's list, writes it, then applies every profile's list.
func editSkills(env *Env, edit func(skills.List) error) error {
	dir, err := skillsProfile(env)
	if err == nil {
		var l skills.List
		if l, err = skills.ReadList(skillsList(dir)); err == nil {
			if err = edit(l); err == nil {
				err = skills.WriteList(skillsList(dir), l)
			}
		}
	}
	if err != nil {
		if _, ok := err.(usageError); ok {
			return err
		}
		return reportErr(env, "skills", err)
	}
	fmt.Fprintf(env.Stdout, "%s updated\n", shortHome(env.Home, skillsList(dir)))
	dirs, err := allProfileDirs(env)
	if err != nil {
		return reportErr(env, "skills", err)
	}
	return applySkills(env, dirs, false)
}

func newSkillsAdoptCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "adopt [-n]",
		Short: "Write the global skills installed from a source into the profile's list",
		Long: "Add every global skill installed from a source to <profile>/home/.config/skills/skills.json,\n" +
			"so that the list takes over the skills already there (-n: print the list without writing it).\n" +
			"Skills without a source are left out: version them with dot adopt. -p is needed with several profiles.",
		Args: exactArgs(0, "dot ai skills adopt [-n]"),
		RunE: func(*cobra.Command, []string) error {
			dir, err := skillsProfile(env)
			if err != nil {
				if _, ok := err.(usageError); ok {
					return err
				}
				return reportErr(env, "skills", err)
			}
			l, err := skills.ReadList(skillsList(dir))
			if err != nil {
				return reportErr(env, "skills", err)
			}
			installed, err := skills.Options{}.State()
			if err != nil {
				return reportErr(env, "skills", err)
			}
			skills.Adopt(l, installed)
			if dry {
				_, err := env.Stdout.Write(skills.Encode(l))
				return err
			}
			if err := skills.WriteList(skillsList(dir), l); err != nil {
				return reportErr(env, "skills", err)
			}
			fmt.Fprintf(env.Stdout, "%s updated\n", shortHome(env.Home, skillsList(dir)))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "print the list without writing it")
	return cmd
}

func newSkillsAddCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "add <source>[#ref] <skill>...",
		Short: "Add skills to the profile's list, then apply the lists",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 2 {
				return usagef("usage: dot ai skills add <source>[#ref] <skill>...")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return editSkills(env, func(l skills.List) error {
				skills.AddTo(l, args[0], args[1:]...)
				_, err := skills.Parse(skills.Encode(l)) // the names the CLI would misread
				return err
			})
		},
	}
}

func newSkillsRmCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <skill>...",
		Short: "Remove skills from the profile's list, then apply the lists",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usagef("usage: dot ai skills rm <skill>...")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return editSkills(env, func(l skills.List) error {
				if missing := skills.RemoveFrom(l, args...); len(missing) > 0 {
					return fmt.Errorf("not in the profile's list: %s", strings.Join(missing, ", "))
				}
				return nil
			})
		},
	}
}

// newSkillsPassCmd runs the pinned skills CLI with the terminal: its own output and prompts.
func newSkillsPassCmd(env *Env, name, short string, args ...string) *cobra.Command {
	return &cobra.Command{
		Use:                name + " [arguments]",
		Short:              short,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, rest []string) error {
			argv := append(append([]string{"-y", skills.CLI}, args...), rest...)
			return runWithStdio(env, exec.Command("npx", argv...))
		},
	}
}
