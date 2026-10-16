package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() { register(newStatusCmd) }

// driftReport lists the home/ files of a profile clone that are no longer links; nil until
// internal/link provides it. Its lines go to stderr after the git status.
var driftReport func(dir string) []string

func newStatusCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   "Changes in the profile clones, and home/ files that are no longer links",
		Long: "For each profile: state of the clone (git status -sb: changes, branch, ahead and behind), then the\n" +
			"home/ files that are no longer links.\n" +
			"Without -p, $DOT_PROFILE or $DOTFILES_DEPLOY, every registered profile is listed, each under a\n" +
			"\"==> <key>\" line. It is a report: the exit code is 0.\n\n" +
			"A \"detached\" file was rewritten by a tool instead of being modified through the link: " +
			"its changes will never reach the repository. Restore the link (dot install) after carrying the content over.",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error {
			batch, err := targetProfiles(env)
			if err != nil {
				return err
			}
			for _, p := range batch {
				if len(batch) > 1 {
					fmt.Fprintf(env.Stdout, "==> %s\n", p.Key)
				}
				// git reports its own failure (missing clone…) on stderr; a report goes on to the next profile.
				if err := runWithStdio(env, gitCmd(p.Dir, "status", "-sb")); err != nil {
					if _, ok := err.(exitError); !ok {
						fmt.Fprintf(env.Stderr, "status %s: %v\n", p.Key, err)
					}
					continue
				}
				if driftReport != nil {
					for _, l := range driftReport(p.Dir) {
						fmt.Fprintln(env.Stderr, l)
					}
				}
			}
			return nil
		},
	}
}
