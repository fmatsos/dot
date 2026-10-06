package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func init() { register(newPushCmd) }

func newPushCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "push [message]",
		Short: "Commite les fichiers suivis modifiés des profils, puis pousse",
		Long: "Pour chaque profil : commite les fichiers suivis modifiés, puis git push.\n\n" +
			"Message par défaut : « Update <fichiers> ». Les fichiers nouveaux ne sont pas inclus : " +
			"dot add <fichier> d'abord. Le garde-fou (secrets, références interdites) tourne au commit et au push.\n" +
			"Sans -p, $DOT_PROFILE ni $DOTFILES_DEPLOY, tous les profils inscrits sont traités (même message pour\n" +
			"tous) ; un profil en échec n'arrête pas les autres, le code de sortie est 1 s'il y en a un.",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			batch, err := targetProfiles(env)
			if err != nil {
				return err
			}
			msg := strings.Join(args, " ")
			var failed []string
			for _, p := range batch {
				if len(batch) > 1 {
					fmt.Fprintf(env.Stdout, "==> %s\n", p.Key)
				}
				if err := pushProfile(env, p.Dir, msg); err != nil {
					if _, ok := err.(exitError); !ok { // a child's failure was already reported by git
						fmt.Fprintf(env.Stderr, "push %s : %v\n", p.Key, err)
					}
					failed = append(failed, p.Key)
				}
			}
			if len(failed) > 0 {
				fmt.Fprintf(env.Stderr, "échec : %s\n", strings.Join(failed, ", "))
				return exitError{code: 1}
			}
			return nil
		},
	}
}

// pushProfile commits the tracked changes of one clone (message from msg, else the first files), then pushes.
func pushProfile(env *Env, dir, msg string) error {
	untracked, err := gitText(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return exitError{code: 1}
	}
	if untracked != "" {
		fmt.Fprintf(env.Stderr, "fichiers nouveaux non suivis (dot add <fichier> pour les inclure) :\n%s\n", untracked)
	}
	if err := runWithStdio(env, gitCmd(dir, "add", "-u")); err != nil {
		return err
	}
	err = gitCmd(dir, "diff", "--cached", "--quiet").Run()
	var ee *exec.ExitError
	if err == nil {
		// A commit left unpushed by an earlier failed push must stay retryable.
		ahead, aerr := gitText(dir, "rev-list", "--count", "@{upstream}..HEAD")
		if aerr != nil || ahead == "0" {
			_, err = fmt.Fprintln(env.Stdout, "rien à envoyer")
			return err
		}
		fmt.Fprintf(env.Stdout, "rien à commiter, %s commit(s) à pousser\n", ahead)
		return runWithStdio(env, gitCmd(dir, "push"))
	} else if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		return exitError{code: 1}
	}
	if msg == "" {
		names, err := gitText(dir, "diff", "--cached", "--name-only")
		if err != nil {
			return exitError{code: 1}
		}
		files := strings.Split(names, "\n")
		files = files[:min(len(files), 5)]
		for i, f := range files {
			files[i] = strings.TrimPrefix(f, "home/")
		}
		msg = "Update " + strings.Join(files, " ")
	}
	if err := runWithStdio(env, gitCmd(dir, "commit", "-m", msg)); err != nil {
		return err
	}
	return runWithStdio(env, gitCmd(dir, "push"))
}
