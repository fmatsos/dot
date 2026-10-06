package main

import (
	"cmp"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/fmatsos/dot/internal/clone"
	"github.com/spf13/cobra"
)

func init() { register(newCloneCmd) }

const cloneUsage = "usage : dot clone [--path] <url> [arguments git clone…]"

// srcRoot is $DOT_SRC, or ~/src.
func srcRoot(env *Env) string { return cmp.Or(env.Getenv("DOT_SRC"), filepath.Join(env.Home, "src")) }

func newCloneCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "clone [--path] <url> [arguments git clone…]",
		Short: "Clone dans ~/src/<hôte>/<propriétaire>/<dépôt>",
		Long: "Clone dans ~/src/<hôte>/<propriétaire>/<dépôt>.\n\n" +
			"Seuls le premier groupe et le nom du dépôt sont gardés (pas les sous-groupes). --path affiche " +
			"la destination sans cloner. Racine : $DOT_SRC (défaut ~/src). Un dépôt déjà cloné au même " +
			"endroit est réutilisé. Les arguments suivants vont à git clone.",
		DisableFlagsInUseLine: true,
		// Everything after the URL belongs to git clone, flags included.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			pathOnly := len(args) > 0 && args[0] == "--path"
			if pathOnly {
				args = args[1:]
			}
			if len(args) < 1 || (pathOnly && len(args) != 1) {
				return usagef(cloneUsage)
			}
			target, err := clone.Path(args[0], srcRoot(env))
			if err != nil {
				return usagef(cloneUsage)
			}
			if pathOnly {
				_, err = fmt.Fprintln(env.Stdout, target)
				return err
			}
			target, reused, err := clone.Clone(args[0], clone.Options{
				Root: srcRoot(env), Args: args[1:], Stdin: env.Stdin, Stdout: env.Stdout, Stderr: env.Stderr,
			})
			var ee *exec.ExitError
			switch {
			case errors.As(err, &ee):
				return exitError{code: max(ee.ExitCode(), 1)}
			case err != nil:
				return err
			case reused:
				_, err = fmt.Fprintf(env.Stdout, "déjà cloné : %s\n", target)
			default:
				_, err = fmt.Fprintln(env.Stdout, target)
			}
			return err
		},
	}
}
