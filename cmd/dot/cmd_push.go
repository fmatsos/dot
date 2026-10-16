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
		Short: "Commit the modified tracked files of the profiles, then push",
		Long: "For each profile: commit the modified tracked files, then git push.\n\n" +
			"Default message: \"Update <files>\". New files are not included: " +
			"dot add <file> first. The guard (secrets, forbidden references) runs at commit and at push.\n" +
			"Without -p, $DOT_PROFILE or $DOTFILES_DEPLOY, every registered profile is processed (same message for\n" +
			"all); a failing profile does not stop the others, the exit code is 1 if there is one.",
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
						fmt.Fprintf(env.Stderr, "push %s: %v\n", p.Key, err)
					}
					failed = append(failed, p.Key)
				}
			}
			if len(failed) > 0 {
				fmt.Fprintf(env.Stderr, "failed: %s\n", strings.Join(failed, ", "))
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
		fmt.Fprintf(env.Stderr, "new untracked files (dot add <file> to include them):\n%s\n", untracked)
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
			_, err = fmt.Fprintln(env.Stdout, "nothing to send")
			return err
		}
		fmt.Fprintf(env.Stdout, "nothing to commit, %s commit(s) to push\n", ahead)
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
