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
	key, err := gitConfigGet("dotfiles.profile")
	if err != nil {
		return "", errors.New("dotfiles.profile unreadable")
	}
	if key != "" {
		if !profileKeyRe.MatchString(key) {
			return "", errors.New("dotfiles.profile invalid")
		}
		return filepath.Join(env.ProfileDirFor(key), "forbidden.local"), nil
	}
	dir, err := env.ProfileDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "forbidden.local"), nil
}

// gitConfigGet reads one key of the current repository's config; empty when the key is unset
// (git exits 1), an error for any other failure.
func gitConfigGet(key string) (string, error) {
	var out bytes.Buffer
	c := exec.Command("git", "config", "--get", key)
	c.Stdout = &out
	var x *exec.ExitError
	if err := c.Run(); err != nil && !(errors.As(err, &x) && x.ExitCode() == 1) {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// guardMode returns "" (terms and secrets) or "secrets" (betterleaks only). $DOTFILES_GUARD wins,
// else `git config dotfiles.guard` of the current repository. Anything else fails closed, the
// value never shown.
func guardMode(env *Env) (string, error) {
	mode, src := env.Getenv("DOTFILES_GUARD"), "$DOTFILES_GUARD"
	if mode == "" {
		var err error
		if mode, err = gitConfigGet("dotfiles.guard"); err != nil {
			return "", errors.New("dotfiles.guard unreadable, refused")
		}
		src = "dotfiles.guard"
	}
	if mode != "" && mode != "secrets" {
		return "", fmt.Errorf("%s: unknown value, refused", src)
	}
	return mode, nil
}

// newGuard builds the guard of the current repository; the "secrets" mode skips the terms.
func newGuard(env *Env) (*guard.Guard, error) {
	g := &guard.Guard{Stderr: env.Stderr, Scan: scanner(env)}
	mode, err := guardMode(env)
	if err != nil {
		return nil, err
	}
	if mode == "secrets" {
		return g, nil
	}
	path, err := termsPath(env)
	if err != nil {
		return nil, fmt.Errorf("forbidden terms list not found (%v), refused", err)
	}
	if g.Terms, err = guard.LoadTerms(path); err != nil {
		if errors.Is(err, guard.ErrAbsent) {
			return nil, fmt.Errorf("forbidden terms list missing or empty (%s), refused", path)
		}
		if errors.Is(err, guard.ErrMatchesEmpty) {
			return nil, fmt.Errorf("forbidden terms list invalid (%s): a term matches the empty string, hence everything (a*, ^, x?…), refused", path)
		}
		return nil, fmt.Errorf("forbidden terms list invalid or unreadable (%s), refused", path)
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
			return errors.New("betterleaks: cannot run")
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
		Short: "Leak guard: secrets and forbidden references",
		Long: "Leak guard for git hooks and CI: secrets (betterleaks) and work references\n" +
			"(terms from a local list, never versioned). Fails closed: a missing, empty or invalid list,\n" +
			"a failing git command or an unavailable scanner all block. Reports only names of files,\n" +
			"branches, tags or commits, never the text found.\n\n" +
			"Images and fonts (png, jpg, gif, webp, avif, heic, ico, woff, woff2) are not read for terms: their\n" +
			"bytes are not text. Their names are, and betterleaks reads them. Any other binary is read.\n\n" +
			"Terms list: case-insensitive extended expressions (RE2 syntax), one per line;\n" +
			"blank lines and lines starting with # are ignored. Escaped GNU operators\n" +
			"(\\| \\+ \\? \\{ \\( \\)) and flags that turn case-insensitivity off ((?-i)) are rejected. Resolution order:\n" +
			"  1. $DOTFILES_FORBIDDEN, the list file;\n" +
			"  2. <profile>/forbidden.local, where the profile directory is, in this order:\n" +
			"     a. $DOTFILES_DEPLOY, if set;\n" +
			"     b. ~/.dot/<p>, where <p> is \"git config dotfiles.profile\" of the current repository;\n" +
			"     c. otherwise the targeted profile: -p, $DOT_PROFILE, then the registry's default profile.\n\n" +
			"$DOTFILES_GUARD=secrets, or \"git config dotfiles.guard secrets\" of the current repository, skips the terms\n" +
			"(betterleaks only): for a repository that cites an employer on purpose. The environment wins;\n" +
			"any other value is rejected.\n\n" +
			"Scanner: $DOT_BETTERLEAKS (executable, for tests), otherwise the pinned copy\n" +
			"~/.cache/dot/betterleaks-<version>/betterleaks, downloaded and verified by sha256 (compiled-in sum).\n\n" +
			"A repository's hooks fit on one line: exec dot guard staged.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usagef("unknown command: guard %s", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "staged",
			Short: "Check the index before a commit (identity, files, secrets)",
			Args:  exactArgs(0, "dot guard staged"),
			RunE:  run(func(g *guard.Guard, _ []string) error { return g.Staged() }),
		},
		&cobra.Command{
			Use:   "msg <file>",
			Short: "Check a commit message",
			Args:  exactArgs(1, "dot guard msg <file>"),
			RunE:  run(func(g *guard.Guard, a []string) error { return g.Msg(a[0]) }),
		},
		&cobra.Command{
			Use:   "push [remote url]",
			Short: "Check a push (reads the pre-push hook lines on stdin)",
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
			Short: "Check all refs and the whole history",
			Args:  exactArgs(0, "dot guard all"),
			RunE:  run(func(g *guard.Guard, _ []string) error { return g.All() }),
		},
	)
	return cmd
}
