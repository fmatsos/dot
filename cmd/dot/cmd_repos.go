package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fmatsos/dot/internal/repos"
	"github.com/spf13/cobra"
)

func init() { register(newReposCmd) }

const reposUsage = "usage: dot repos [--problems | export [file] | clone [file]]"

// useColor is true on a terminal unless NO_COLOR is set.
// ponytail: any character device but /dev/null counts as a terminal, no ioctl.
func useColor(env *Env, w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || env.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(fi, null)
}

func sandboxRoot(env *Env) string {
	return cmp.Or(env.Getenv("DOT_SANDBOX"), filepath.Join(env.Home, "sandbox"))
}

func newReposCmd(env *Env) *cobra.Command {
	var problems bool
	// listFile is the explicit file argument, or <profile>/repos.local.
	listFile := func(args []string) (string, error) {
		if len(args) > 0 {
			return args[0], nil
		}
		dir, err := env.ProfileDir()
		return filepath.Join(dir, "repos.local"), err
	}
	atMostOne := func(_ *cobra.Command, args []string) error {
		if len(args) > 1 {
			return usagef(reposUsage)
		}
		return nil
	}
	cmd := &cobra.Command{
		Use:   "repos [--problems]",
		Short: "Status of local repositories, export and re-clone of their remotes",
		Long: "Status of local repositories, without fetch, and a list of remotes for another machine.\n\n" +
			"With no argument: branch, changes, ahead/behind and identity of each repository. Roots: " +
			"$DOT_SRC (default ~/src, depth 3) and $DOT_SANDBOX (~/sandbox, depth 1). Worktrees " +
			"are excluded. An identity different from the profile's is flagged.\n\n" +
			"--problems: inconsistent identities or bypassed guard; exit 1.",
		DisableFlagsInUseLine: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef(reposUsage)
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			rows := repos.Collect(env.Home, srcRoot(env), sandboxRoot(env), true)
			bad := false
			shown := rows[:0:0]
			for _, r := range rows {
				bad = bad || r.Reason != ""
				if !problems || r.Reason != "" {
					shown = append(shown, r)
				}
			}
			for _, l := range repos.Format(shown, useColor(env, env.Stdout)) {
				fmt.Fprintln(env.Stdout, l)
			}
			if problems && bad {
				return exitError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&problems, "problems", false, "show only inconsistent identities or the bypassed guard (exit 1)")
	cmd.AddCommand(
		&cobra.Command{
			Use:   "export [file]",
			Short: "Keep the unique origin remotes, sorted, in a private file",
			Long: "Keep the unique origin remotes, sorted, in a private file (0600).\n\n" +
				"Default: <profile directory>/repos.local. Sandbox repositories are not exported.",
			Args: atMostOne,
			RunE: func(_ *cobra.Command, args []string) error {
				file, err := listFile(args)
				if err != nil {
					return err
				}
				n, err := repos.Export(srcRoot(env), file)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(env.Stdout, "%d remotes → %s\n", n, shortHome(env.Home, file))
				return err
			},
		},
		&cobra.Command{
			Use:   "clone [file]",
			Short: "Re-clone this list, continuing after an error",
			Long: "Re-clone this list, continuing after an error.\n\n" +
				"Same default file; repositories already present are reused by dot clone.",
			Args: atMostOne,
			RunE: func(_ *cobra.Command, args []string) error {
				file, err := listFile(args)
				if err != nil {
					return err
				}
				good, bad, err := repos.CloneList(file, srcRoot(env), env.Stderr)
				if errors.Is(err, repos.ErrUnreadable) {
					return fmt.Errorf("repos: %w", err)
				} else if err != nil {
					return err
				}
				fmt.Fprintf(env.Stdout, "%d repositories reused or cloned, %d failures\n", good, bad)
				if bad > 0 {
					return exitError{code: 1}
				}
				return nil
			},
		},
	)
	for _, c := range cmd.Commands() {
		c.DisableFlagsInUseLine = true
	}
	return cmd
}
