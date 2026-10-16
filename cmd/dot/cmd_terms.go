package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fmatsos/dot/internal/clone"
	"github.com/spf13/cobra"
)

func init() { register(newTermsCmd) }

var blankOrComment = regexp.MustCompile(`^\s*(#|$)`)

func newTermsCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:                   "terms",
		Short:                 "Copy the local forbidden terms list into the CI secret",
		Long:                  "Copy the local forbidden terms list (the profile's forbidden.local) into the CI secret FORBIDDEN_TERMS of the origin repository, through gh, values on stdin.",
		DisableFlagsInUseLine: true,
		Args:                  exactArgs(0, "dot terms"),
		RunE: func(*cobra.Command, []string) error {
			dir, err := env.ProfileDir()
			if err != nil {
				return err
			}
			origin, err := gitText(dir, "config", "--local", "--get", "remote.origin.url")
			if err != nil {
				return fmt.Errorf("terms: origin missing or invalid URL")
			}
			// Mapping against "." keeps <host>/<owner>/<repo>, the form gh expects for -R.
			repo, err := clone.Path(origin, ".")
			if err != nil {
				return fmt.Errorf("terms: origin missing or invalid URL")
			}
			var terms []string
			if data, err := os.ReadFile(filepath.Join(dir, "forbidden.local")); err == nil {
				for _, l := range strings.Split(string(data), "\n") {
					if !blankOrComment.MatchString(l) {
						terms = append(terms, l)
					}
				}
			}
			if len(terms) == 0 {
				return fmt.Errorf("terms: list missing, unreadable or empty")
			}
			// The terms travel on stdin, never on argv.
			c := exec.Command("gh", "secret", "set", "FORBIDDEN_TERMS", "-R", repo)
			c.Stdin = strings.NewReader(strings.Join(terms, "\n") + "\n")
			if err := runWithStdio(env, c); err != nil {
				return err
			}
			_, err = fmt.Fprintln(env.Stdout, "secret FORBIDDEN_TERMS updated")
			return err
		},
	}
}
