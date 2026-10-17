package main

import (
	"github.com/fmatsos/dot/internal/selfupdate"
	"github.com/spf13/cobra"
)

func init() { register(newSelfUpdateCmd) }

func newSelfUpdateCmd(env *Env) *cobra.Command {
	var dry bool
	var target string
	cmd := &cobra.Command{
		Use:   "self-update [--version vX.Y.Z] [-n]",
		Short: "Update dot from a signed GitHub release",
		Long: "Download the release (the latest, or --version vX.Y.Z), verify the Ed25519 signature of\n" +
			"SHA256SUMS with the key compiled into the binary, then the sha256 of the binary, and replace\n" +
			"the running executable atomically. Any failed check aborts without changing\n" +
			"anything. Without a signing key in the binary, the update is refused.\n" +
			"-n: show what would be done, without downloading a binary or writing anything; without --version,\n" +
			"the latest release is still requested from the GitHub API, to know which one to install.\n" +
			"$GITHUB_TOKEN or $GH_TOKEN, when set, is sent to api.github.com only, to raise its rate limit.\n\n" +
			"$DOT_RELEASE_BASE and $DOT_RELEASE_API override the release URLs, for tests only.",
		Args: exactArgs(0, "dot self-update [--version vX.Y.Z] [-n]"),
		RunE: func(*cobra.Command, []string) error {
			return selfupdate.Run(selfupdate.Options{
				Current: version, Target: target, Dry: dry, Out: env.Stdout,
				BaseURL: env.Getenv("DOT_RELEASE_BASE"), APIURL: env.Getenv("DOT_RELEASE_API"),
			})
		},
	}
	cmd.Flags().StringVar(&target, "version", "", "version to install, vX.Y.Z (default: the latest release)")
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show what would be done without writing anything")
	return cmd
}
