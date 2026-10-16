package main

import (
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/spf13/cobra"
)

// helpColor is set by setupHelp for the template functions below: bold headers, cyan command and
// flag names, on a terminal only (see useColor).
var helpColor atomic.Bool

// flagHead is the "-p, --profile string" part of a pflag usage line, up to the gap before its description.
var flagHead = regexp.MustCompile(`(?m)^(\s+)(-\S+(?:, --\S+)?(?: [^\s-]\S*)?)(\s{2,}|$)`)

func init() {
	paint := func(code string) func(string) string {
		return func(s string) string {
			if !helpColor.Load() {
				return s
			}
			return "\x1b[" + code + "m" + s + "\x1b[0m"
		}
	}
	cobra.AddTemplateFunc("helpHead", paint("1"))
	cobra.AddTemplateFunc("helpName", paint("36"))
	cobra.AddTemplateFunc("helpFlags", colorFlags)
}

// colorFlags paints the option names of pflag's usage lines, nothing else.
func colorFlags(s string) string {
	if !helpColor.Load() {
		return s
	}
	return flagHead.ReplaceAllString(s, "$1\x1b[36m$2\x1b[0m$3")
}

// usageTemplate is cobra's default usage template, simplified and lowercased.
const usageTemplate = `{{helpHead "usage:"}} {{if and .Runnable .HasParent}}{{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}{{if and .Runnable .HasParent}}
        {{end}}{{.CommandPath}} <command> [arguments]{{end}}{{if gt (len .Aliases) 0}}

{{helpHead "aliases:"}} {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{helpHead "examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

{{helpHead "commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{helpName (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{helpHead "options:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces | helpFlags}}{{end}}{{if .HasAvailableInheritedFlags}}

{{helpHead "global options:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces | helpFlags}}{{end}}{{if and .HasAvailableSubCommands (not .HasParent)}}

` + "`{{.CommandPath}} help <command>`" + ` shows a command's help.{{end}}
`

// setupHelp gives the whole tree the usage template above and a help command that reports
// unknown topics with exit code 2 and also answers for extensions (dot-<cmd> --help).
func setupHelp(env *Env, root *cobra.Command) {
	helpColor.Store(useColor(env, env.Stdout))
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
