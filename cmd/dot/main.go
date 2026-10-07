package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var version = "dev"

// usageError marks wrong arguments or flags: exit code 2 instead of 1.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// exitError carries a specific exit code (a wrapped child's, or 3 for a locked vault).
// An empty msg means the command already reported its error.
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// exactArgs wraps an argument count check into a usageError carrying the command's usage line.
func exactArgs(n int, usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return usagef("usage : %s", usage)
		}
		return nil
	}
}

func newRoot(env *Env) *cobra.Command {
	root := &cobra.Command{
		Use:           "dot",
		Short:         "dot installe et entretient un ou plusieurs profils de dotfiles",
		Long:          "dot installe et entretient un ou plusieurs profils de dotfiles : liens dans ~, garde-fou contre les fuites, secrets, serveurs MCP et réglages.\n\nUn profil est un dépôt de données cloné dans ~/.dot/<clé>, inscrit dans ~/.dot/profiles.json.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("commande inconnue : %s", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetIn(env.Stdin)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.SetVersionTemplate("dot {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err.Error()} })
	root.PersistentFlags().StringVarP(&env.Profile, "profile", "p", "", "profil visé (sinon $DOT_PROFILE, puis le profil par défaut)")
	root.RegisterFlagCompletionFunc("profile", completeProfileKeys(env))
	root.InitDefaultHelpFlag()
	root.Flags().Lookup("help").Usage = "affiche l'aide"
	root.InitDefaultVersionFlag()
	root.Flags().Lookup("version").Usage = "affiche la version"
	for _, f := range constructors {
		root.AddCommand(f(env))
	}
	frenchHelp(env, root)
	return root
}

// execute runs one invocation and returns the exit code.
func execute(env *Env, args []string) int {
	root := newRoot(env)
	args, external := route(env, root, append([]string{}, args...))
	var err error
	if external {
		err = runExternal(env, args)
	} else {
		root.SetArgs(args)
		err = root.Execute()
	}
	if err == nil {
		return 0
	}
	var x exitError
	if errors.As(err, &x) {
		if x.msg != "" {
			fmt.Fprintf(env.Stderr, "dot : %s\n", x.msg)
		}
		return x.code
	}
	var u usageError
	if errors.As(err, &u) {
		if strings.HasPrefix(u.msg, "usage :") {
			fmt.Fprintln(env.Stderr, u.msg)
		} else {
			fmt.Fprintf(env.Stderr, "dot : %s\n", u.msg)
		}
		return 2
	}
	fmt.Fprintf(env.Stderr, "dot : %v\n", err)
	return 1
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dot : dossier personnel introuvable ($HOME)")
		os.Exit(1)
	}
	os.Exit(execute(&Env{Home: home, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}, os.Args[1:]))
}
