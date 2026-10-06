package main

import (
	"errors"
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

// findExtension looks for an executable dot-<name> in the profile's bin/, then in the PATH.
func findExtension(env *Env, name string) (path string, deploy string) {
	if !extensionName.MatchString(name) {
		return "", ""
	}
	if dir, err := env.ProfileDir(); err == nil {
		deploy = dir
		p := filepath.Join(dir, "bin", "dot-"+name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, deploy
		}
	}
	if p, err := exec.LookPath("dot-" + name); err == nil {
		return p, deploy
	}
	return "", deploy
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
	if path, deploy := findExtension(env, name); path != "" {
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
