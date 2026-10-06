package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fmatsos/dot/internal/guard"
	"github.com/spf13/cobra"
)

func init() { register(newGuardCmd) }

var profileKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// termsPath resolves the forbidden-terms list: $DOTFILES_FORBIDDEN, else forbidden.local in the
// profile directory (see the command help for how that directory is chosen).
func termsPath(env *Env) (string, error) {
	if f := env.Getenv("DOTFILES_FORBIDDEN"); f != "" {
		return env.Expand(f), nil
	}
	if d := env.Getenv("DOTFILES_DEPLOY"); d != "" {
		return filepath.Join(env.Expand(d), "forbidden.local"), nil
	}
	var out bytes.Buffer
	c := exec.Command("git", "config", "--get", "dotfiles.profile")
	c.Stdout = &out
	var x *exec.ExitError
	if err := c.Run(); err != nil && !(errors.As(err, &x) && x.ExitCode() == 1) { // 1 = key unset
		return "", errors.New("dotfiles.profile illisible")
	}
	if key := strings.TrimSpace(out.String()); key != "" {
		if !profileKeyRe.MatchString(key) {
			return "", errors.New("dotfiles.profile invalide")
		}
		return filepath.Join(env.ProfileDirFor(key), "forbidden.local"), nil
	}
	dir, err := env.ProfileDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "forbidden.local"), nil
}

// newGuard builds the guard of the current repository; DOTFILES_GUARD=secrets skips the terms.
func newGuard(env *Env) (*guard.Guard, error) {
	g := &guard.Guard{Stderr: env.Stderr, Scan: scanner(env)}
	if env.Getenv("DOTFILES_GUARD") == "secrets" {
		return g, nil
	}
	path, err := termsPath(env)
	if err != nil {
		return nil, fmt.Errorf("liste de termes interdits introuvable (%v), refus.", err)
	}
	if g.Terms, err = guard.LoadTerms(path); err != nil {
		if errors.Is(err, guard.ErrAbsent) {
			return nil, fmt.Errorf("liste de termes interdits absente ou vide (%s), refus.", path)
		}
		return nil, fmt.Errorf("liste de termes interdits invalide ou illisible (%s), refus.", path)
	}
	return g, nil
}

// scanner runs betterleaks: $DOT_BETTERLEAKS (tests), else the pinned copy in ~/.cache/dot.
func scanner(env *Env) func(args ...string) error {
	return func(args ...string) error {
		exe := env.Getenv("DOT_BETTERLEAKS")
		if exe == "" {
			var err error
			if exe, err = guard.Betterleaks.Ensure(filepath.Join(env.Home, ".cache", "dot"), guard.Platform(), nil); err != nil {
				return err
			}
		}
		c := exec.Command(exe, append([]string{"--no-banner", "--redact"}, args...)...)
		c.Stdout, c.Stderr = env.Stderr, env.Stderr
		err := c.Run()
		var x *exec.ExitError
		if err != nil && !errors.As(err, &x) {
			return errors.New("betterleaks : lancement impossible")
		}
		return err
	}
}

func newGuardCmd(env *Env) *cobra.Command {
	run := func(f func(g *guard.Guard, args []string) error) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error {
			g, err := newGuard(env)
			if err == nil {
				err = f(g, args)
			}
			if err == nil {
				return nil
			}
			msg := err.Error()
			var fl *guard.Failure
			if errors.As(err, &fl) {
				msg = fl.Msg
			}
			fmt.Fprintf(env.Stderr, "guard: %s\n", msg)
			return exitError{code: 1}
		}
	}
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Garde-fou anti-fuite : secrets et références interdites",
		Long: "Garde-fou anti-fuite des hooks git et de la CI : secrets (betterleaks) et références au travail\n" +
			"(termes d'une liste locale, jamais versionnée). Échoue fermé : liste absente, vide ou invalide,\n" +
			"commande git en échec ou scanner indisponible bloquent. Ne rapporte que des noms de fichiers,\n" +
			"de branches, de tags ou des commits, jamais le texte trouvé.\n\n" +
			"Liste des termes : des expressions étendues insensibles à la casse (syntaxe RE2), une par ligne ;\n" +
			"les lignes vides et celles qui commencent par # sont ignorées. Les opérateurs GNU échappés\n" +
			"(\\| \\+ \\? \\{ \\( \\)) et les drapeaux coupant la casse ((?-i)) sont refusés. Ordre de résolution :\n" +
			"  1. $DOTFILES_FORBIDDEN, le fichier de la liste ;\n" +
			"  2. <profil>/forbidden.local, où le dossier du profil est, dans cet ordre :\n" +
			"     a. $DOTFILES_DEPLOY, s'il est défini ;\n" +
			"     b. ~/.dot/<p>, où <p> est « git config dotfiles.profile » du dépôt courant ;\n" +
			"     c. sinon le profil ciblé : -p, $DOT_PROFILE, puis le profil par défaut du registre.\n\n" +
			"$DOTFILES_GUARD=secrets ignore les termes (betterleaks seul) : pour un dépôt qui cite un employeur\n" +
			"volontairement.\n\n" +
			"Scanner : $DOT_BETTERLEAKS (exécutable, pour les tests), sinon la copie figée\n" +
			"~/.cache/dot/betterleaks-<version>/betterleaks, téléchargée et vérifiée par sha256 (somme compilée).\n\n" +
			"Les hooks d'un dépôt tiennent en une ligne : exec dot guard staged.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("commande inconnue : guard %s", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "staged",
			Short: "Contrôle l'index avant un commit (identité, fichiers, secrets)",
			Args:  exactArgs(0, "dot guard staged"),
			RunE:  run(func(g *guard.Guard, _ []string) error { return g.Staged() }),
		},
		&cobra.Command{
			Use:   "msg <fichier>",
			Short: "Contrôle un message de commit",
			Args:  exactArgs(1, "dot guard msg <fichier>"),
			RunE:  run(func(g *guard.Guard, a []string) error { return g.Msg(a[0]) }),
		},
		&cobra.Command{
			Use:   "push [distant url]",
			Short: "Contrôle un push (lit sur stdin les lignes du hook pre-push)",
			Args:  cobra.ArbitraryArgs, // git hands the hook the remote name and URL: only the name is used
			RunE: run(func(g *guard.Guard, a []string) error {
				remote := ""
				if len(a) > 0 {
					remote = a[0]
				}
				return g.Push(env.Stdin, remote)
			}),
		},
		&cobra.Command{
			Use:   "all",
			Short: "Contrôle toutes les refs et tout l'historique",
			Args:  exactArgs(0, "dot guard all"),
			RunE:  run(func(g *guard.Guard, _ []string) error { return g.All() }),
		},
	)
	return cmd
}
