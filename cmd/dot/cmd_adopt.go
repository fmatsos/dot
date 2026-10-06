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

// sparseHasLayer refuses an adoption into a clone whose sparse set leaves the layer (home or a
// home@ variant) out.
func sparseHasLayer(dir, layer string) error {
	m, err := manifest.Load(filepath.Join(dir, "dot.json"))
	if err != nil {
		if _, serr := os.Stat(filepath.Join(dir, "dot.json")); serr != nil {
			return nil // no manifest: nothing says the clone is sparse
		}
		return err
	}
	if len(m.Sparse) > 0 && !slices.Contains(m.Sparse, layer) {
		return fmt.Errorf("%s/ est hors du sparse checkout du profil (deploy.sparse : %s)", layer, strings.Join(m.Sparse, ", "))
	}
	return nil
}

func newAdoptCmd(env *Env) *cobra.Command {
	var dry, onOS, onHost bool
	cmd := &cobra.Command{
		Use:   "adopt [-p <clé>] [-n] [--os | --host] <fichier>...",
		Short: "Range des fichiers existants de ~ dans un profil et les remplace par des liens",
		Long: "Pour chaque fichier ordinaire de ~ : le déplace vers <profil>/home/<même chemin> et crée à sa place le lien\n" +
			"que dot install aurait créé. Le profil visé est -p, $DOT_PROFILE, puis le défaut du registre.\n" +
			"--os range dans home@<système>/ (darwin, linux), --host dans home@<machine>/ : variantes de home/ qui ne\n" +
			"valent que pour ce système ou cette machine (le sparse checkout doit les lister).\n\n" +
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
			layer := "home"
			switch {
			case onOS && onHost:
				return usagef("adopt : --os et --host s'excluent")
			case onOS:
				layer = link.OSLayer()
			case onHost:
				var ok bool
				if layer, ok = link.HostLayer(); !ok {
					return errors.New("adopt : --host : nom de machine inconnu ou invalide")
				}
			}
			dir, err := env.ProfileDir()
			if err != nil {
				return err
			}
			dir, _ = filepath.Abs(dir)
			key := filepath.Base(dir)
			if _, err := os.Stat(filepath.Join(dir, layer)); err != nil {
				if fi, serr := os.Stat(dir); serr != nil || !fi.IsDir() {
					return fmt.Errorf("adopt : clone absent (%s)", dir)
				}
			}
			if err := sparseHasLayer(dir, layer); err != nil {
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
				dst := filepath.Join(dir, layer, rel)
				if _, err := os.Lstat(dst); err == nil {
					refuse(tilde, fmt.Sprintf("destination existante (%s:%s/%s)", key, layer, filepath.ToSlash(rel)))
					continue
				}
				if above, ok := link.ShadowedBy(dir, layer, rel); ok {
					refuse(tilde, fmt.Sprintf("masqué par %s:%s/%s, qui l'emporte sur %s", key, above, filepath.ToSlash(rel), layer))
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
				fmt.Fprintf(env.Stdout, "adopt %s -> %s:%s/%s\n", tilde, key, layer, filepath.ToSlash(rel))
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
	cmd.Flags().BoolVar(&onOS, "os", false, "range dans home@<système>/ (variante de ce système)")
	cmd.Flags().BoolVar(&onHost, "host", false, "range dans home@<machine>/ (variante de cette machine)")
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "affiche le plan et contrôle sans rien écrire")
	return cmd
}
