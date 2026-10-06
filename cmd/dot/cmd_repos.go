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

const reposUsage = "usage : dot repos [--problems | export [fichier] | clone [fichier]]"

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
		Short: "État des dépôts locaux, export et reclonage de leurs remotes",
		Long: "État des dépôts locaux, sans fetch, et liste de remotes pour une autre machine.\n\n" +
			"Sans argument : branche, changements, avance/retard et identité de chaque dépôt. Racines : " +
			"$DOT_SRC (défaut ~/src, profondeur 3) et $DOT_SANDBOX (~/sandbox, profondeur 1). Les worktrees " +
			"sont exclus. Une identité différente de celle du profil est signalée.\n\n" +
			"--problems : identités incohérentes ou garde-fou contourné ; code 1.",
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
	cmd.Flags().BoolVar(&problems, "problems", false, "n'affiche que les identités incohérentes ou le garde-fou contourné (code 1)")
	cmd.AddCommand(
		&cobra.Command{
			Use:   "export [fichier]",
			Short: "Garde les remotes origin uniques, triés, dans un fichier privé",
			Long: "Garde les remotes origin uniques, triés, dans un fichier privé (0600).\n\n" +
				"Défaut : <dossier du profil>/repos.local. Les dépôts sandbox ne sont pas exportés.",
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
			Use:   "clone [fichier]",
			Short: "Reclone cette liste, en continuant après une erreur",
			Long: "Reclone cette liste, en continuant après une erreur.\n\n" +
				"Même fichier par défaut ; les dépôts déjà présents sont réutilisés par dot clone.",
			Args: atMostOne,
			RunE: func(_ *cobra.Command, args []string) error {
				file, err := listFile(args)
				if err != nil {
					return err
				}
				good, bad, err := repos.CloneList(file, srcRoot(env), env.Stderr)
				if errors.Is(err, repos.ErrUnreadable) {
					return fmt.Errorf("repos : %w", err)
				} else if err != nil {
					return err
				}
				fmt.Fprintf(env.Stdout, "%d dépôts réutilisés ou clonés, %d échecs\n", good, bad)
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
