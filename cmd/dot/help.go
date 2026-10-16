package main

import (
	"strings"

	"github.com/spf13/cobra"
)

// usageTemplate is cobra's default usage template, simplified and lowercased.
const usageTemplate = `usage: {{if and .Runnable .HasParent}}{{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}{{if and .Runnable .HasParent}}
        {{end}}{{.CommandPath}} <command> [arguments]{{end}}{{if gt (len .Aliases) 0}}

aliases: {{.NameAndAliases}}{{end}}{{if .HasExample}}

examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

options:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

global options:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if and .HasAvailableSubCommands (not .HasParent)}}

` + "`{{.CommandPath}} help <command>`" + ` shows a command's help.{{end}}
`

// setupHelp gives the whole tree the usage template above and a help command that reports
// unknown topics with exit code 2 and also answers for extensions (dot-<cmd> --help).
func setupHelp(env *Env, root *cobra.Command) {
	root.SetUsageTemplate(usageTemplate)
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [command]",
		Short: "Show the help of a command",
		RunE: func(_ *cobra.Command, args []string) error {
			target, rest, err := root.Find(args)
			if err == nil && len(rest) == 0 && target != nil {
				return target.Help()
			}
			if len(args) == 1 {
				if path, deploy, err := findExtension(env, args[0]); err != nil {
					return err
				} else if path != "" {
					return runExtension(env, path, deploy, []string{"--help"})
				}
			}
			return usagef("unknown command: %s", strings.Join(args, " "))
		},
	})
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() != "completion" {
			continue
		}
		c.Short, c.Long = "Generate the completion script for a shell", "Generate the completion script for a shell (bash, zsh, fish, powershell)."
		for _, sub := range c.Commands() {
			sub.Short = "Generate the completion script for " + sub.Name()
			sub.Long = sub.Short + "."
			if f := sub.Flags().Lookup("no-descriptions"); f != nil {
				f.Usage = "disable completion descriptions"
			}
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = "show the help"
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}
