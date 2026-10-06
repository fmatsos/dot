// Package clone maps a remote URL to its place under the source root and clones it there.
package clone

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidURL = errors.New("URL non prise en charge")
	ErrOccupied   = errors.New("destination déjà occupée")

	urlRe = regexp.MustCompile(`^(?:https|ssh)://(?:[^/@:[:space:]]+@)?([[:alnum:]][[:alnum:].-]*)(?::[0-9]+)?/(.+)$`)
	scpRe = regexp.MustCompile(`^[^/@:[:space:]]+@([[:alnum:]][[:alnum:].-]*):(.+)$`)
)

// Path returns root/<host>/<owner>/<repo> for a remote URL (https, ssh or scp-like).
// Only the first group and the repository name are kept: sub-groups are dropped.
func Path(url, root string) (string, error) {
	var host, path string
	if m := urlRe.FindStringSubmatch(url); m != nil {
		host, path = m[1], m[2]
	} else if m := scpRe.FindStringSubmatch(url); m != nil {
		host, path = m[1], m[2]
	} else {
		return "", ErrInvalidURL
	}
	path = strings.TrimSuffix(path, "/")
	if strings.ContainsAny(path, " \t\n\r\f\v?#") || strings.HasPrefix(path, "/") ||
		strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return "", ErrInvalidURL
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", ErrInvalidURL
	}
	for _, s := range parts {
		if s == "" || s == "." || s == ".." {
			return "", ErrInvalidURL
		}
	}
	repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
	if repo == "" || repo == "." || repo == ".." {
		return "", ErrInvalidURL
	}
	return filepath.Join(root, strings.ToLower(host), parts[0], repo), nil
}

// Options tunes Clone. Nil writers discard git's output; nil Env inherits the process one.
type Options struct {
	Root           string
	Args           []string // extra arguments for git clone, placed before the URL
	Env            []string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Clone clones url into its mapped place and returns the destination. An already cloned
// repository whose origin maps to the same place is reused (reused = true). A failing git
// is reported as its *exec.ExitError, git having already said why on Stderr.
func Clone(url string, o Options) (target string, reused bool, err error) {
	target, err = Path(url, o.Root)
	if err != nil {
		return "", false, err
	}
	if _, err := os.Lstat(target); err == nil {
		if sameOrigin(target, o) {
			return target, true, nil
		}
		return target, false, fmt.Errorf("clone : %w : %s", ErrOccupied, target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return target, false, err
	}
	cmd := exec.Command("git", append(append([]string{"clone"}, o.Args...), url, target)...)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = o.Env, o.Stdin, o.Stdout, o.Stderr
	return target, false, cmd.Run()
}

func git(env []string, dir string, args ...string) (string, bool) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	return strings.TrimSuffix(string(out), "\n"), err == nil
}

// sameOrigin tells whether target is a repository (or a bare one) whose origin maps to target.
func sameOrigin(target string, o Options) bool {
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		if bare, _ := git(o.Env, target, "rev-parse", "--is-bare-repository"); bare != "true" {
			return false
		}
	}
	if _, ok := git(o.Env, target, "rev-parse", "--git-dir"); !ok {
		return false
	}
	origin, ok := git(o.Env, target, "config", "--local", "--get", "remote.origin.url")
	if !ok {
		return false
	}
	got, err := Path(origin, o.Root)
	return err == nil && got == target
}
