package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	register(newStatusCmd)
	register(newSendCmd)
}

// driftReport lists the home/ files of a profile clone that are no longer links; nil until
// internal/link provides it. Its lines go to stderr after the git status.
var driftReport func(dir string) []string

func newStatusCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:     "st",
		Aliases: []string{"status"},
		Short:   "État du clone du profil, et fichiers de home/ qui ne sont plus des liens",
		Long: "État du clone du profil (git status -sb), et fichiers de home/ qui ne sont plus des liens.\n\n" +
			"Un fichier « détaché » a été réécrit par un outil au lieu d'être modifié à travers le lien : " +
			"ses changements n'iront jamais dans le dépôt. Remets le lien (dot install) après avoir reporté le contenu.",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error {
			dir, err := env.ProfileDir()
			if err != nil {
				return err
			}
			if err := runWithStdio(env, gitCmd(dir, "status", "-sb")); err != nil {
				return err
			}
			if driftReport != nil {
				for _, l := range driftReport(dir) {
					fmt.Fprintln(env.Stderr, l)
				}
			}
			return nil
		},
	}
}

func newSendCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "send [message]",
		Short: "Commite les fichiers suivis modifiés, puis pousse",
		Long: "Commite les fichiers suivis modifiés, puis pousse.\n\n" +
			"Message par défaut : « Update <fichiers> ». Les fichiers nouveaux ne sont pas inclus : " +
			"dot add <fichier> d'abord. Le garde-fou (secrets, références interdites) tourne au commit et au push.",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			dir, err := env.ProfileDir()
			if err != nil {
				return err
			}
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
				_, err = fmt.Fprintln(env.Stdout, "rien à envoyer")
				return err
			} else if !errors.As(err, &ee) || ee.ExitCode() != 1 {
				return exitError{code: 1}
			}
			msg := strings.Join(args, " ")
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
		},
	}
}
