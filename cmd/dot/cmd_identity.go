package main

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	register(newWhoamiCmd)
	register(newProfileCmd)
}

var (
	profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	remoteGlob  = regexp.MustCompile(`://|@.*:`)
)

func newWhoamiCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:                   "whoami",
		Short:                 "Identité git en vigueur ici, et le fichier qui la fixe",
		DisableFlagsInUseLine: true,
		Args:                  exactArgs(0, "dot whoami"),
		RunE: func(*cobra.Command, []string) error {
			if err := runWithStdio(env, exec.Command("git", "config", "--show-origin", "user.email")); err != nil {
				fmt.Fprintln(env.Stderr, "aucun email : useConfigOnly bloquera les commits ici")
			}
			return runWithStdio(env, exec.Command("git", "var", "GIT_AUTHOR_IDENT"))
		},
	}
}

func newProfileCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "profile <nom> <dossier/|url-du-remote>",
		Short: "Applique un profil git à un dossier ou un remote",
		Long: "Applique un profil git à un dossier ou un remote.\n\n" +
			"Écrit la règle includeIf dans ~/.config/git/profiles.local (jamais versionné). Le profil est " +
			"~/.config/git/profiles/<nom>.gitconfig ou <nom>.local. Le fichier peut fixer dotfiles.profile = <nom> : " +
			"dot repos vérifie alors son email.",
		Example: "  dot profile demo ~/sandbox/essai/\n  dot profile demo 'git@github.com:moi/**'",
		Args:    exactArgs(2, "dot profile <nom> <dossier/|url-du-remote>"),
		RunE: func(_ *cobra.Command, args []string) error {
			name, target := args[0], args[1]
			if !profileName.MatchString(name) || strings.Contains(name, "..") {
				return fmt.Errorf("profil : nom invalide")
			}
			profiles := filepath.Join(env.Home, ".config", "git", "profiles")
			file := filepath.Join(profiles, name+".gitconfig")
			if fi, err := os.Stat(file); err != nil || !fi.Mode().IsRegular() {
				file = filepath.Join(profiles, name+".local")
			}
			if fi, err := os.Stat(file); err != nil || !fi.Mode().IsRegular() {
				return fmt.Errorf("profil introuvable : ~/.config/git/profiles/%s.{gitconfig,local}", name)
			}
			var cond string
			if remoteGlob.MatchString(target) {
				cond = "hasconfig:remote.*.url:" + target
			} else {
				// A relative target is taken from the working directory, never from $CDPATH.
				d, err := filepath.Abs(target)
				if err != nil {
					return err
				}
				if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
					return fmt.Errorf("profile : dossier introuvable : %s", target)
				}
				cond = "gitdir:" + d + "/"
			}
			rules := filepath.Join(env.Home, ".config", "git", "profiles.local")
			header := `[includeIf "` + cond + `"]`
			want := shortHome(env.Home, file)
			if path, found := rulePath(rules, header); found {
				if path != want {
					return fmt.Errorf("profile : une règle existe déjà pour %s vers %s : la retirer de ~/.config/git/profiles.local", cond, cmp.Or(path, "(sans path)"))
				}
				_, err := fmt.Fprintf(env.Stdout, "règle déjà présente : %s → %s\n", cond, name)
				return err
			}
			f, err := os.OpenFile(rules, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			_, werr := fmt.Fprintf(f, "%s\n\tpath = %s\n", header, want)
			if err := f.Close(); werr == nil {
				werr = err
			}
			if werr != nil {
				return werr
			}
			_, err = fmt.Fprintf(env.Stdout, "règle ajoutée : %s → %s\n", cond, name)
			return err
		},
	}
}

// rulePath finds the includeIf block starting with header in file and returns its path value;
// found is false when no such block exists.
func rulePath(file, header string) (path string, found bool) {
	f, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == header:
			found = true
		case strings.HasPrefix(line, "["):
			if found {
				return path, true
			}
		case found:
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "path" {
				path = strings.TrimSpace(v)
			}
		}
	}
	return path, found
}
