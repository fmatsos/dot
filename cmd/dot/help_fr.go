package main

import (
	"strings"

	"github.com/spf13/cobra"
)

// usageTemplate is cobra's default usage template, translated.
const usageTemplate = `usage : {{if and .Runnable .HasParent}}{{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}{{if and .Runnable .HasParent}}
        {{end}}{{.CommandPath}} <commande> [arguments]{{end}}{{if gt (len .Aliases) 0}}

alias : {{.NameAndAliases}}{{end}}{{if .HasExample}}

exemples :
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

commandes :{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

options :
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

options globales :
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if and .HasAvailableSubCommands (not .HasParent)}}

« {{.CommandPath}} help <commande> » détaille une commande.{{end}}
`

// frenchHelp gives the whole tree a French usage text and a help command that reports
// unknown topics with exit code 2 and also answers for extensions (dot-<cmd> --help).
func frenchHelp(env *Env, root *cobra.Command) {
	root.SetUsageTemplate(usageTemplate)
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [commande]",
		Short: "Affiche l'aide d'une commande",
		RunE: func(_ *cobra.Command, args []string) error {
			target, rest, err := root.Find(args)
			if err == nil && len(rest) == 0 && target != nil {
				return target.Help()
			}
			if len(args) == 1 {
				if path, deploy := findExtension(env, args[0]); path != "" {
					return runExtension(env, path, deploy, []string{"--help"})
				}
			}
			return usagef("commande inconnue : %s", strings.Join(args, " "))
		},
	})
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() != "completion" {
			continue
		}
		c.Short, c.Long = "Génère le script de complétion pour un shell", "Génère le script de complétion pour un shell (bash, zsh, fish, powershell)."
		for _, sub := range c.Commands() {
			sub.Short = "Génère le script de complétion pour " + sub.Name()
			sub.Long = sub.Short + "."
			if f := sub.Flags().Lookup("no-descriptions"); f != nil {
				f.Usage = "désactive la description des complétions"
			}
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = "affiche l'aide"
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}
