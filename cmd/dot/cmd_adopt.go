package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fmatsos/dot/internal/guard"
	"github.com/fmatsos/dot/internal/link"
	"github.com/fmatsos/dot/internal/manifest"
	"github.com/spf13/cobra"
)

func init() { register(newAdoptCmd) }

// adoptGuard builds the guard of the target profile: its forbidden.local ($DOTFILES_FORBIDDEN
// overrides it, like for dot guard), or secrets only when $DOTFILES_GUARD or the clone's
// dotfiles.guard says so. A missing or invalid list refuses.
func adoptGuard(env *Env, dir string) (*guard.Guard, error) {
	g := &guard.Guard{Stderr: env.Stderr, Scan: scanner(env)}
	mode, src := env.Getenv("DOTFILES_GUARD"), "$DOTFILES_GUARD"
	if mode == "" {
		var err error
		if mode, err = gitText(dir, "config", "--get", "dotfiles.guard"); err != nil {
			mode = ""
		}
		src = "dotfiles.guard"
	}
	if mode != "" && mode != "secrets" {
		return nil, fmt.Errorf("%s : valeur inconnue, refus", src)
	}
	if mode == "secrets" {
		return g, nil
	}
	path := filepath.Join(dir, "forbidden.local")
	if f := env.Getenv("DOTFILES_FORBIDDEN"); f != "" {
		path = env.Expand(f)
	}
	var err error
	if g.Terms, err = guard.LoadTerms(path); err != nil {
		if errors.Is(err, guard.ErrAbsent) {
			return nil, fmt.Errorf("liste de termes interdits absente ou vide (%s), refus", path)
		}
		return nil, fmt.Errorf("liste de termes interdits invalide ou illisible (%s), refus", path)
	}
	return g, nil
}

// sparseHasHome refuses an adoption into a clone whose sparse set leaves home/ out.
func sparseHasHome(dir string) error {
	m, err := manifest.Load(filepath.Join(dir, "dot.json"))
	if err != nil {
		if _, serr := os.Stat(filepath.Join(dir, "dot.json")); serr != nil {
			return nil // no manifest: nothing says the clone is sparse
		}
		return err
	}
	if len(m.Sparse) > 0 && !slices.Contains(m.Sparse, "home") {
		return fmt.Errorf("home/ est hors du sparse checkout du profil (deploy.sparse : %s)", strings.Join(m.Sparse, ", "))
	}
	return nil
}

func newAdoptCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "adopt [-p <clé>] [-n] <fichier>...",
		Short: "Range des fichiers existants de ~ dans un profil et les remplace par des liens",
		Long: "Pour chaque fichier ordinaire de ~ : le déplace vers <profil>/home/<même chemin> et crée à sa place le lien\n" +
			"que dot install aurait créé. Le profil visé est -p, $DOT_PROFILE, puis le défaut du registre.\n\n" +
			"Refus (exit 1, les autres fichiers continuent) : hors de ~ ou dans ~/.dot, lien symbolique (un fichier déjà\n" +
			"lié aussi), dossier, destination existante, fichier déjà lié par un autre profil, home/ hors du sparse\n" +
			"checkout, terme interdit de la liste du profil visé, secret (betterleaks). Le contrôle précède tout\n" +
			"déplacement et ne rapporte que le nom du fichier. Rien n'est commité : dot push s'en charge.\n\n" +
			"-n affiche le plan et lance les contrôles sans rien écrire.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usagef("usage : dot adopt [-p <clé>] [-n] <fichier>...")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			dir, err := env.ProfileDir()
			if err != nil {
				return err
			}
			dir, _ = filepath.Abs(dir)
			key := filepath.Base(dir)
			if _, err := os.Stat(filepath.Join(dir, "home")); err != nil {
				if fi, serr := os.Stat(dir); serr != nil || !fi.IsDir() {
					return fmt.Errorf("adopt : clone absent (%s)", dir)
				}
			}
			if err := sparseHasHome(dir); err != nil {
				return fmt.Errorf("adopt : %v", err)
			}
			g, err := adoptGuard(env, dir)
			if err != nil {
				return fmt.Errorf("adopt : %v", err)
			}
			var others []link.Profile
			if keys, err := env.ProfileKeys(); err == nil {
				for _, k := range keys {
					if d, _ := filepath.Abs(env.ProfileDirFor(k)); d != dir {
						others = append(others, link.Profile{Key: k, Dir: d})
					}
				}
			}
			cwd, _ := os.Getwd()
			failed, done := false, 0
			for _, arg := range args {
				shown := func(s string) string { // a name holding a term is never printed
					if g.Terms.Match(s) {
						return "<nom masqué>"
					}
					return s
				}
				refuse := func(shownPath, why string) {
					fmt.Fprintf(env.Stderr, "adopt : %s refusé : %s\n", shown(shownPath), why)
					failed = true
				}
				src, rel, err := link.AdoptSource(env.Home, cwd, env.Expand(arg))
				if err != nil {
					refuse(arg, err.Error())
					continue
				}
				tilde := link.Tilde(env.Home, src)
				dst := filepath.Join(dir, "home", rel)
				if _, err := os.Lstat(dst); err == nil {
					refuse(tilde, fmt.Sprintf("destination existante (%s:home/%s)", key, filepath.ToSlash(rel)))
					continue
				}
				if owners := link.ClaimedBy(rel, others); len(owners) > 0 {
					refuse(tilde, fmt.Sprintf("fichier lié par plusieurs profils : %s (home/%s) et %s", strings.Join(owners, ", "), filepath.ToSlash(rel), key))
					continue
				}
				if err := g.File(src, tilde); err != nil {
					msg := err.Error()
					var fl *guard.Failure
					if errors.As(err, &fl) {
						msg = fl.Msg
					}
					refuse(tilde, msg)
					continue
				}
				fmt.Fprintf(env.Stdout, "adopt %s -> %s:home/%s\n", tilde, key, filepath.ToSlash(rel))
				if dry {
					continue
				}
				if err := link.AdoptMove(src, dst); err != nil {
					refuse(tilde, err.Error())
					continue
				}
				done++
			}
			if failed {
				return exitError{code: 1}
			}
			if done > 0 {
				fmt.Fprintln(env.Stdout, "adopté : rien n'est commité, dot push pour commiter.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "affiche le plan et contrôle sans rien écrire")
	return cmd
}
