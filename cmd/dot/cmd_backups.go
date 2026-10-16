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
		Short: "List and restore files backed up by dot install",
		Long: "dot install moves each existing file it replaces with a link into\n" +
			"~/.local/state/dotfiles/backup/<date>/ (one directory per install). \"dot backups list\" lists them,\n" +
			"\"dot backups restore\" puts them back.",
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
		Short: "List backups, most recent first",
		Long: "Lists the installs that backed up files, most recent first, with the paths\n" +
			"(relative to ~) of their files. By default only the first few are shown; --all shows them all,\n" +
			"and a <run> id (YYYYMMDD-HHMMSS) lists only that install, in full.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usagef("usage: dot backups list [--all] [<run>]")
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
				fmt.Fprintln(env.Stdout, "no backups")
				return nil
			}
			for _, r := range runs {
				fmt.Fprintf(env.Stdout, "%s  %d file(s)\n", r.ID, len(r.Files))
				for i, f := range r.Files {
					if !all && i == listPreview {
						fmt.Fprintf(env.Stdout, "  … and %d more (--all)\n", len(r.Files)-listPreview)
						break
					}
					fmt.Fprintf(env.Stdout, "  %s\n", link.Tilde(env.Home, filepath.Join(env.Home, filepath.FromSlash(f))))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "show all the files of each backup")
	return cmd
}

func newBackupsRestoreCmd(env *Env) *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "restore [<run>] [<path>...] [-n]",
		Short: "Put backed-up files back (default: the latest backup, in full)",
		Long: "Moves (without copying) the files of a backup to ~, keeping their mode. <run> is an id\n" +
			"YYYYMMDD-HHMMSS (default: the most recent); <path> limits to files or directories, relative to ~ or as ~/….\n\n" +
			"A file is restored only if its place in ~ is free or held by a dot link (to a registered\n" +
			"profile), which the restore removes. Any other file is refused and reported, never overwritten;\n" +
			"the others continue, and the exit code is 1 if anything was refused. The emptied backup directory is removed.\n\n" +
			"Note: \"dot install\" recreates the links. For the restore to stick, run \"dot uninstall\" first\n" +
			"or remove the file from the profile. -n shows the plan without writing anything.",
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
					return fmt.Errorf("no backup to restore")
				}
				run = runs[0].ID
			}
			var clones []string
			if keys, err := env.ProfileKeys(); err == nil {
				for _, k := range keys {
					clones = append(clones, env.ProfileDirFor(k))
				}
			}
			fmt.Fprintf(env.Stdout, "backup %s\n", run)
			n, refused, err := link.Restore(link.RestoreRequest{Home: env.Home, Run: run, Paths: args, Clones: clones, Dry: dry, Out: env.Stdout, Err: env.Stderr})
			if err != nil {
				return err
			}
			if n > 0 {
				fmt.Fprintln(env.Stdout, "note: dot install will recreate the links; run dot uninstall, or remove the file from the profile, for the restore to stick")
			}
			if refused > 0 {
				return exitError{code: 1, msg: fmt.Sprintf("%d file(s) refused", refused)}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "show the plan without writing anything")
	return cmd
}
