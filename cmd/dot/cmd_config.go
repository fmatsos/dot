package main

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/fmatsos/dot/internal/registry"
	"github.com/spf13/cobra"
)

func init() { register(newConfigCmd) }

// usageIfKey maps a malformed config key to a usage error (exit 2).
func usageIfKey(err error) error {
	if errors.Is(err, registry.ErrUnknownKey) {
		return usageError{err.Error()}
	}
	return err
}

func newConfigCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and edit the profile registry (~/.dot/profiles.json)",
		Long: "Read and edit the profile registry (~/.dot/profiles.json).\n\n" +
			"Keys: default, profiles.<key>.repo",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("unknown command: config %s", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	getCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Show a value",
		Args:  exactArgs(1, "dot config get <key>"),
		RunE: func(_ *cobra.Command, args []string) error {
			r, err := env.Registry()
			if err != nil {
				return err
			}
			v, err := r.Get(args[0])
			if err != nil {
				return usageIfKey(err)
			}
			_, err = fmt.Fprintln(env.Stdout, v)
			return err
		},
	}
	getCmd.ValidArgsFunction = completeConfigKeys(env)

	unsetCmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a value (a profile still installed is refused)",
		Args:  exactArgs(1, "dot config unset <key>"),
		RunE: func(_ *cobra.Command, args []string) error {
			r, err := env.Registry()
			if err != nil {
				return err
			}
			if p, ok := registry.ProfileKey(args[0]); ok && slices.Contains(r.Keys(), p) {
				if _, err := os.Stat(env.ProfileDirFor(p)); err == nil {
					return fmt.Errorf("profile %s is still installed (%s): uninstall it before forgetting it", p, env.ProfileDirFor(p))
				}
			}
			if err := r.Unset(args[0]); err != nil {
				return usageIfKey(err)
			}
			return r.Save(env.RegistryPath())
		},
	}
	unsetCmd.ValidArgsFunction = completeConfigKeys(env)

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "Show the registry (JSON)",
			Args:  exactArgs(0, "dot config list"),
			RunE: func(*cobra.Command, []string) error {
				r, err := env.Registry()
				if err != nil {
					return err
				}
				data, err := r.JSON()
				if err != nil {
					return err
				}
				_, err = env.Stdout.Write(data)
				return err
			},
		},
		getCmd,
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Write a validated value, atomically",
			Args:  exactArgs(2, "dot config set <key> <value>"),
			RunE: func(_ *cobra.Command, args []string) error {
				r, err := env.Registry()
				if err != nil {
					return err
				}
				if err := r.Set(args[0], args[1]); err != nil {
					return usageIfKey(err)
				}
				return r.Save(env.RegistryPath())
			},
		},
		unsetCmd,
	)
	return cmd
}
