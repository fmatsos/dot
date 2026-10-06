package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var extensionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// route strips the global -p/--profile placed before the command (recording it in env.Profile),
// and tells whether the first word is no cobra command: it is then an extension or a git command.
// Cobra's own flags (--help, --version) and its hidden commands stay with cobra.
func route(env *Env, root *cobra.Command, args []string) (rest []string, external bool) {
	i := 0
loop:
	for i < len(args) {
		a := args[i]
		switch {
		case (a == "-p" || a == "--profile") && i+1 < len(args):
			env.Profile = args[i+1]
			i += 2
		case strings.HasPrefix(a, "--profile="):
			env.Profile = strings.TrimPrefix(a, "--profile=")
			i++
		case strings.HasPrefix(a, "-p") && !strings.HasPrefix(a, "--") && len(a) > 2:
			env.Profile = strings.TrimPrefix(a[2:], "=")
			i++
		default:
			break loop
		}
	}
	rest = args[i:]
	if len(rest) == 0 || strings.HasPrefix(rest[0], "-") || strings.HasPrefix(rest[0], "__") ||
		rest[0] == "help" || rest[0] == "completion" {
		return rest, false
	}
	c, _, _ := root.Find(rest[:1])
	return rest, c == root
}

// findExtension looks for an executable dot-<name> in a profile's bin/, then in the PATH.
// Without -p, DOT_PROFILE or DOTFILES_DEPLOY and with several profiles registered, every
// profile's bin/ is searched: a name claimed by two profiles is refused.
func findExtension(env *Env, name string) (path string, deploy string, err error) {
	if !extensionName.MatchString(name) {
		return "", "", nil
	}
	if dir, err := env.ProfileDir(); err == nil {
		deploy = dir
	}
	targeted := env.Profile != "" || env.Getenv("DOT_PROFILE") != "" || env.Getenv("DOTFILES_DEPLOY") != ""
	keys, kerr := env.ProfileKeys()
	if targeted || kerr != nil || len(keys) <= 1 {
		if deploy != "" {
			if p := filepath.Join(deploy, "bin", "dot-"+name); isExecutable(p) {
				return p, deploy, nil
			}
		}
	} else {
		var owners []string
		for _, k := range keys {
			if p := filepath.Join(env.ProfileDirFor(k), "bin", "dot-"+name); isExecutable(p) {
				owners, path = append(owners, k), p
			}
		}
		switch len(owners) {
		case 1:
			return path, env.ProfileDirFor(owners[0]), nil
		case 0:
		default:
			return "", "", fmt.Errorf("extension dot-%s revendiquée par plusieurs profils (%s) : précisez -p <clé>", name, strings.Join(owners, ", "))
		}
	}
	if p, err := exec.LookPath("dot-" + name); err == nil {
		return p, deploy, nil
	}
	return "", deploy, nil
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// runWithStdio runs a child with the env's standard streams (stdin unless already set); its exit status becomes an exitError.
func runWithStdio(env *Env, c *exec.Cmd) error {
	if c.Stdin == nil {
		c.Stdin = env.Stdin
	}
	c.Stdout, c.Stderr = env.Stdout, env.Stderr
	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return exitError{code: max(ee.ExitCode(), 1)}
	}
	return err
}

func runExtension(env *Env, path, deploy string, args []string) error {
	c := exec.Command(path, args...)
	c.Env = os.Environ()
	if deploy != "" {
		c.Env = append(c.Env, "DOTFILES_DEPLOY="+deploy)
	}
	return runWithStdio(env, c)
}

// runExternal runs `dot <name> args…`: the extension dot-<name> if any, else git on the profile clone.
func runExternal(env *Env, args []string) error {
	name := args[0]
	if path, deploy, err := findExtension(env, name); err != nil {
		return err
	} else if path != "" {
		return runExtension(env, path, deploy, args[1:])
	}
	dir, err := env.ProfileDir()
	if err != nil {
		return usagef("commande inconnue : %s", name)
	}
	return runWithStdio(env, gitCmd(dir, args...))
}

func gitCmd(dir string, args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-C", dir}, args...)...)
}

// gitText runs git on dir and returns its trimmed stdout.
func gitText(dir string, args ...string) (string, error) {
	out, err := gitCmd(dir, args...).Output()
	return strings.TrimSuffix(string(out), "\n"), err
}

// shortHome shortens a path under home to ~.
func shortHome(home, p string) string {
	if p == home || strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
