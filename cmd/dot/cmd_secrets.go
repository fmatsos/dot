package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/fmatsos/dot/internal/secrets"
	"github.com/spf13/cobra"
)

func init() { register(newSecretsCmd) }

// Swapped by tests: the real ones replace the process and read /dev/tty.
var (
	execProcess  = syscall.Exec
	promptHidden = secrets.PromptHidden
)

// secretFail reports err the way dot secrets always did: the sentinel messages (locked vault,
// logged-out Proton Pass) as is with exit code 3, anything else as "secret : …" with exit code 1.
// The text never holds a value.
func secretFail(env *Env, err error) error {
	if errors.Is(err, secrets.ErrLocked) || errors.Is(err, secrets.ErrPassLoggedOut) {
		fmt.Fprintln(env.Stderr, err)
		return exitError{code: 3}
	}
	fmt.Fprintf(env.Stderr, "secret : %v\n", err)
	return exitError{code: 1}
}

func secretFailf(env *Env, format string, a ...any) error {
	return secretFail(env, fmt.Errorf(format, a...))
}

func newSecretsCmd(env *Env) *cobra.Command {
	store := func() (*secrets.Store, error) {
		dir, err := env.ProfileDir()
		if err != nil {
			return nil, err
		}
		return secrets.New(dir), nil
	}
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Lit les secrets au moment où une commande en a besoin",
		Long: "dot secrets — lit les secrets au moment où une commande en a besoin, jamais dans le shell.\n\n" +
			"Les noms viennent de secrets.local, dans le dossier du profil : NOM=bw:<élément> ou\n" +
			"NOM=pass:<coffre>/<élément>. Les valeurs ne passent jamais en argument de processus.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "help" && len(args) == 1 {
				return nil
			}
			return usagef("commande inconnue : secrets %s", args[0])
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "add NOM <réf>",
			Short: "Crée un secret ou réutilise un élément existant",
			Long: "Crée un secret ou réutilise un élément existant.\n\n" +
				"Référence : bw:<élément> ou pass:<coffre>/<élément>. Saisie cachée deux fois au terminal,\n" +
				"sinon stdin (un retour à la ligne final retiré). La valeur est vérifiée avant l'ajout à\n" +
				"secrets.local (600) ; un élément existant ne demande aucune saisie.",
			Args: exactArgs(2, "dot secrets add NOM <réf>"),
			RunE: func(cmd *cobra.Command, args []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				created, err := st.Add(cmd.Context(), args[0], args[1], func() (string, error) { return readNewValue(env) })
				if err != nil {
					return secretFail(env, err)
				}
				if created {
					fmt.Fprintf(env.Stdout, "%s : élément créé et référence enregistrée\n", args[0])
				} else {
					fmt.Fprintf(env.Stdout, "%s : élément existant réutilisé, référence enregistrée\n", args[0])
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "get NOM",
			Short: "Affiche la valeur, sans retour à la ligne final",
			Long: "Affiche la valeur, sans retour à la ligne final.\n\n" +
				"Pour une substitution : VAR=$(dot secrets get NOM). Bitwarden verrouillé : code 3.",
			Args: exactArgs(1, "dot secrets get NOM"),
			RunE: func(cmd *cobra.Command, args []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				v, err := st.Get(cmd.Context(), args[0])
				if err != nil {
					return secretFail(env, err)
				}
				_, err = io.WriteString(env.Stdout, v)
				return err
			},
		},
		&cobra.Command{
			Use:   "run NOM... -- CMD...",
			Short: "Lance CMD avec ces variables, et seulement elle",
			Long: "Lance CMD avec ces variables, et seulement elle.\n\n" +
				"Toutes les valeurs sont lues avant le lancement ; le code de sortie de CMD est conservé.",
			Args: func(cmd *cobra.Command, args []string) error {
				if d := cmd.ArgsLenAtDash(); d < 1 || len(args) <= d {
					return usagef("usage : dot secrets run NOM... -- CMD...")
				}
				return nil
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				d := cmd.ArgsLenAtDash()
				// Resolve everything first: a manager never inherits the other fetched values.
				kv, err := st.Resolve(cmd.Context(), args[:d])
				if err != nil {
					return secretFail(env, err)
				}
				argv := args[d:]
				path, err := exec.LookPath(argv[0])
				if err != nil {
					code := 126
					if errors.Is(err, exec.ErrNotFound) {
						code = 127
					}
					return exitError{code, fmt.Sprintf("%s : commande introuvable", argv[0])}
				}
				err = execProcess(path, argv, secrets.OverrideEnv(os.Environ(), kv))
				return exitError{126, fmt.Sprintf("%s : lancement impossible (%v)", argv[0], err)}
			},
		},
		&cobra.Command{
			Use:   "unlock",
			Short: "Déverrouille Bitwarden et garde la session (fichier 600)",
			Long: "Déverrouille Bitwarden et garde la session (fichier 600).\n\n" +
				"Le mot de passe maître est demandé dans le terminal et passé à bw par l'environnement.",
			Args: exactArgs(0, "dot secrets unlock"),
			RunE: func(cmd *cobra.Command, _ []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				pw, err := promptHidden("Mot de passe maître Bitwarden : ")
				if err != nil {
					return secretFailf(env, "Bitwarden : saisie annulée")
				}
				if err := st.Unlock(cmd.Context(), pw); err != nil {
					return secretFail(env, err)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "lock",
			Short: "Verrouille Bitwarden et efface la session gardée",
			Args:  exactArgs(0, "dot secrets lock"),
			RunE: func(cmd *cobra.Command, _ []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				if err := st.Lock(cmd.Context()); err != nil {
					return secretFail(env, err)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Indique quels secrets sont lisibles, sans afficher les valeurs",
			Long: "Indique quels secrets sont lisibles, sans afficher les valeurs.\n\n" +
				"Un état par secret : lisible, verrouillé ou indisponible.",
			Args: exactArgs(0, "dot secrets status"),
			RunE: func(cmd *cobra.Command, _ []string) error {
				st, err := store()
				if err != nil {
					return err
				}
				entries, err := st.Status(cmd.Context())
				if err != nil {
					return secretFail(env, err)
				}
				for _, e := range entries {
					fmt.Fprintln(env.Stdout, e)
				}
				return nil
			},
		},
	)
	return cmd
}

// readNewValue asks for the value twice on the terminal, else reads stdin minus one final newline.
func readNewValue(env *Env) (string, error) {
	if secrets.IsTerminal(env.Stdin) {
		v, err := promptHidden("Valeur du secret : ")
		if err != nil {
			return "", errors.New("saisie annulée")
		}
		c, err := promptHidden("Confirme la valeur : ")
		if err != nil {
			return "", errors.New("saisie annulée")
		}
		if v != c {
			return "", errors.New("les valeurs ne correspondent pas")
		}
		return v, nil
	}
	b, err := io.ReadAll(env.Stdin)
	if err != nil {
		return "", errors.New("lecture de stdin impossible")
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}
