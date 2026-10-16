package main

import "github.com/spf13/cobra"

func init() { register(newUninstallCmd) }

const uninstallUsage = "usage: dot uninstall -p <key> [--purge]"

func newUninstallCmd(env *Env) *cobra.Command {
	var purge, force bool
	cmd := &cobra.Command{
		Use:   "uninstall -p <key> [--purge]",
		Short: "Remove a profile's links and its registry entry (the clone is kept)",
		Long: "Remove the profile's links from ~, then its registry entry. The clone ~/.dot/<key> is kept unless\n" +
			"--purge, which deletes it (refused if it has uncommitted changes, unless --force). The default profile\n" +
			"can only go if it is the last one.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 || env.Profile == "" {
				return usagef(uninstallUsage)
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			return newInstaller(env, false).Uninstall(env.Profile, purge, force)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the clone")
	cmd.Flags().BoolVar(&force, "force", false, "with --purge: delete even with uncommitted changes")
	return cmd
}
