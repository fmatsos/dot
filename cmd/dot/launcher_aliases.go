package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

const launcherFile = "launcher.conf"

var launcherName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// splitAliasLine reads "<alias> <command line>"; comments and blank lines give an empty name.
func splitAliasLine(line string) (name, command string) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' {
		return "", ""
	}
	i := strings.IndexAny(t, " \t")
	if i < 0 {
		return t, ""
	}
	return t[:i], strings.TrimSpace(t[i:])
}

// launcherAliases reads <profile>/launcher.conf (versioned): one "<alias> <command line>" per line,
// # for comments. The command line is run by sh -c, so quotes, pipes and && work.
// ponytail: a later line wins; no terminal, write xdg-terminal-exec <cmd> to see the output.
func launcherAliases(file string) (names []string, cmds map[string]string, err error) {
	b, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	cmds = map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		n, c := splitAliasLine(l)
		if n == "" || c == "" {
			continue
		}
		if _, dup := cmds[n]; !dup {
			names = append(names, n)
		}
		cmds[n] = c
	}
	return names, cmds, nil
}

// launcherEdit adds, updates or removes one alias and keeps every other line, comments included.
// An update replaces the first line of the alias and drops later duplicates.
func launcherEdit(path, op, name, command string) error {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var lines []string
	if s := strings.TrimRight(string(b), "\n"); s != "" {
		lines = strings.Split(s, "\n")
	}
	out := []string{}
	found := false
	for _, l := range lines {
		if n, _ := splitAliasLine(l); n != name {
			out = append(out, l)
			continue
		}
		if op == "update" && !found {
			out = append(out, name+" "+command)
		}
		found = true
	}
	switch {
	case op == "add" && found:
		return usagef("alias already defined: %s (see dot launcher update)", name)
	case op != "add" && !found:
		return usagef("unknown alias: %s (see dot launcher list)", name)
	case op == "add":
		out = append(out, name+" "+command)
	}
	content := ""
	if len(out) > 0 {
		content = strings.Join(out, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func newLauncherAliasCmds(env *Env) []*cobra.Command {
	file := func() (string, error) {
		dir, err := env.ProfileDir()
		return filepath.Join(dir, launcherFile), err
	}
	complete := func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if f, err := file(); err == nil && len(args) == 0 {
			names, _, _ := launcherAliases(f)
			return names, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	// write is add or update: the command line is the rest of the arguments, joined by spaces.
	write := func(op, short, long string) *cobra.Command {
		c := &cobra.Command{
			Use:   op + " <alias> <command…>",
			Short: short,
			Long: long + "\n\nThe command is run by sh -c: quote it to include spaces, " +
				"pipes or &&. It does not open a terminal: write xdg-terminal-exec <command> to see its output.",
			Args: func(_ *cobra.Command, args []string) error {
				if len(args) < 2 {
					return usagef("usage: dot launcher %s <alias> <command…>", op)
				}
				return nil
			},
			ValidArgsFunction: complete,
			RunE: func(_ *cobra.Command, args []string) error {
				if !launcherName.MatchString(args[0]) {
					return usagef("invalid alias name: %s (letters, digits, . _ -)", args[0])
				}
				command := strings.TrimSpace(strings.Join(args[1:], " "))
				if command == "" || strings.ContainsAny(command, "\r\n") {
					return usagef("the command must fit on one line")
				}
				f, err := file()
				if err != nil {
					return err
				}
				return launcherEdit(f, op, args[0], command)
			},
		}
		// What follows the alias belongs to the command, flags included.
		c.Flags().SetInterspersed(false)
		return c
	}
	one := func(use, short string, run func(f, name string) error) *cobra.Command {
		return &cobra.Command{
			Use:               use + " <alias>",
			Short:             short,
			Args:              exactArgs(1, "dot launcher "+use+" <alias>"),
			ValidArgsFunction: complete,
			RunE: func(_ *cobra.Command, args []string) error {
				f, err := file()
				if err != nil {
					return err
				}
				return run(f, args[0])
			},
		}
	}
	return []*cobra.Command{
		write("add", "Add an alias", "Add an alias to <profile>/launcher.conf (versioned): typing its name in GNOME search runs the command."),
		write("update", "Replace the command of an alias", "Replace the command of an alias of <profile>/launcher.conf."),
		one("rm", "Remove an alias", func(f, name string) error { return launcherEdit(f, "rm", name, "") }),
		one("get", "Show the command of an alias", func(f, name string) error {
			_, cmds, err := launcherAliases(f)
			if err != nil {
				return err
			}
			c, ok := cmds[name]
			if !ok {
				return usagef("unknown alias: %s (see dot launcher list)", name)
			}
			fmt.Fprintln(env.Stdout, c)
			return nil
		}),
		{
			Use:   "list",
			Short: "List the aliases",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				f, err := file()
				if err != nil {
					return err
				}
				names, cmds, err := launcherAliases(f)
				for _, n := range names {
					fmt.Fprintf(env.Stdout, "%s\t%s\n", n, cmds[n])
				}
				return err
			},
		},
	}
}
