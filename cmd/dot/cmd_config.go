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
		Short: "Lit et modifie le registre des profils (~/.dot/profiles.json)",
		Long: "Lit et modifie le registre des profils (~/.dot/profiles.json).\n\n" +
			"Clés : default, profiles.<clé>.repo",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("commande inconnue : config %s", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	getCmd := &cobra.Command{
		Use:   "get <clé>",
		Short: "Affiche une valeur",
		Args:  exactArgs(1, "dot config get <clé>"),
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
		Use:   "unset <clé>",
		Short: "Retire une valeur (un profil encore installé est refusé)",
		Args:  exactArgs(1, "dot config unset <clé>"),
		RunE: func(_ *cobra.Command, args []string) error {
			r, err := env.Registry()
			if err != nil {
				return err
			}
			if p, ok := registry.ProfileKey(args[0]); ok && slices.Contains(r.Keys(), p) {
				if _, err := os.Stat(env.ProfileDirFor(p)); err == nil {
					return fmt.Errorf("le profil %s est encore installé (%s) : le désinstaller avant de l'oublier", p, env.ProfileDirFor(p))
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
			Short: "Affiche le registre (JSON)",
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
			Use:   "set <clé> <valeur>",
			Short: "Écrit une valeur validée, de façon atomique",
			Args:  exactArgs(2, "dot config set <clé> <valeur>"),
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
