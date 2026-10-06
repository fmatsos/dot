package main

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/fmatsos/dot/internal/link"
	"github.com/spf13/cobra"
)

func init() { register(newBackupsCmd) }

// runIDRe tells a run id (a backup directory stamp) from a path.
var runIDRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}`)

const listPreview = 3

func newBackupsCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backups",
		Short: "Liste et restaure les fichiers sauvegardés par dot install",
		Long: "dot install déplace chaque fichier existant qu'il remplace par un lien dans\n" +
			"~/.local/state/dotfiles/backup/<date>/ (un dossier par installation). « dot backups list » les liste,\n" +
			"« dot backups restore » les remet en place.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newBackupsListCmd(env), newBackupsRestoreCmd(env))
	return cmd
}

func newBackupsListCmd(env *Env) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list [--all] [<run>]",
		Short: "Liste les sauvegardes, la plus récente d'abord",
		Long: "Liste les installations qui ont sauvegardé des fichiers, la plus récente d'abord, avec les chemins\n" +
			"(relatifs à ~) de leurs fichiers. Par défaut seuls les premiers sont montrés ; --all les montre tous,\n" +
			"et un identifiant <run> (AAAAMMJJ-HHMMSS) ne liste que cette installation, en entier.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usagef("usage : dot backups list [--all] [<run>]")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			runs, err := link.ListBackups(env.Home)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				files, err := link.RunFiles(env.Home, args[0])
				if err != nil {
					return err
				}
				runs, all = []link.BackupRun{{ID: args[0], Files: files}}, true
			}
			if len(runs) == 0 {
				fmt.Fprintln(env.Stdout, "aucune sauvegarde")
				return nil
			}
			for _, r := range runs {
				fmt.Fprintf(env.Stdout, "%s  %d fichier(s)\n", r.ID, len(r.Files))
				for i, f := range r.Files {
					if !all && i == listPreview {
						fmt.Fprintf(env.Stdout, "  … et %d autre(s) (--all)\n", len(r.Files)-listPreview)
						break
					}
					fmt.Fprintf(env.Stdout, "  %s\n", link.Tilde(env.Home, filepath.Join(env.Home, filepath.FromSlash(f))))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "montre tous les fichiers de chaque sauvegarde")
	return cmd
}

func newBackupsRestoreCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "restore [<run>] [<chemin>...] [-n]",
		Short: "Remet en place des fichiers sauvegardés (par défaut : la dernière sauvegarde, en entier)",
		Long: "Déplace (sans copier) les fichiers d'une sauvegarde vers ~, en gardant leur mode. <run> est un identifiant\n" +
			"AAAAMMJJ-HHMMSS (défaut : la plus récente) ; <chemin> limite à des fichiers ou dossiers, relatifs à ~ ou en ~/….\n\n" +
			"Un fichier n'est restauré que si sa place dans ~ est libre ou occupée par un lien de dot (vers un profil\n" +
			"inscrit), que la restauration retire. Tout autre fichier est refusé et signalé, sans jamais être écrasé ;\n" +
			"les autres continuent, et le code de sortie est 1 s'il y a eu un refus. Le dossier de sauvegarde vidé est supprimé.\n\n" +
			"Attention : « dot install » recrée les liens. Pour que la restauration tienne, fais d'abord « dot uninstall »\n" +
			"ou retire le fichier du profil. -n affiche le plan sans rien écrire.",
		RunE: func(_ *cobra.Command, args []string) error {
			var run string
			if len(args) > 0 && runIDRe.MatchString(args[0]) {
				run, args = args[0], args[1:]
			}
			if run == "" {
				runs, err := link.ListBackups(env.Home)
				if err != nil {
					return err
				}
				if len(runs) == 0 {
					return fmt.Errorf("aucune sauvegarde à restaurer")
				}
				run = runs[0].ID
			}
			var clones []string
			if keys, err := env.ProfileKeys(); err == nil {
				for _, k := range keys {
					clones = append(clones, env.ProfileDirFor(k))
				}
			}
			fmt.Fprintf(env.Stdout, "sauvegarde %s\n", run)
			n, refused, err := link.Restore(link.RestoreRequest{Home: env.Home, Run: run, Paths: args, Clones: clones, Dry: dry, Out: env.Stdout, Err: env.Stderr})
			if err != nil {
				return err
			}
			if n > 0 {
				fmt.Fprintln(env.Stdout, "note : dot install recréera les liens ; dot uninstall, ou retirer le fichier du profil, pour que la restauration tienne")
			}
			if refused > 0 {
				return exitError{code: 1, msg: fmt.Sprintf("%d fichier(s) refusé(s)", refused)}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "affiche le plan sans rien écrire")
	return cmd
}
