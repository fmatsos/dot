// Package repos reports the state of local git repositories, exports their remotes and clones a list.
// ponytail: fixed-depth directory scans and git plumbing; no registry to maintain.
package repos

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/fmatsos/dot/internal/clone"
)

// ErrUnreadable means the list of remotes to clone is missing or unreadable.
var ErrUnreadable = errors.New("liste absente ou illisible")

const (
	reasonEmail = "✗ email différent du profil "
	reasonHooks = "✗ hooksPath local : garde-fou contourné"
)

// Row is the state of one repository. Reason is empty when its identity is consistent.
type Row struct {
	Path, Branch   string
	Changes        int
	Ahead, Behind  int
	Profile, Email string
	Reason         string
	display        string
}

// Env is the environment git runs with: never prompt, never take optional locks.
func Env() []string { return append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0") }

func git(dir string, args ...string) (string, bool) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = Env()
	out, err := cmd.Output()
	return strings.TrimSuffix(string(out), "\n"), err == nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// children lists the directories at exactly depth levels under root, in name order.
func children(root string, depth int) []string {
	if depth == 0 {
		return []string{root}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if isDir(p) {
			out = append(out, children(p, depth-1)...)
		}
	}
	return out
}

// Find lists the repositories under src (depth 3) and, if asked, sandbox (depth 1).
// Linked worktrees are skipped (their .git is a file) and so are duplicates of one git-common-dir.
func Find(src, sandbox string, withSandbox bool) []string {
	candidates := children(src, 3)
	if withSandbox {
		candidates = append(candidates, children(sandbox, 1)...)
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range candidates {
		if !isDir(filepath.Join(p, ".git")) {
			if bare, _ := git(p, "rev-parse", "--is-bare-repository"); bare != "true" {
				continue
			}
		}
		common, ok := git(p, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if !ok || seen[common] {
			continue
		}
		seen[common] = true
		out = append(out, p)
	}
	return out
}

func tilde(home, p string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + p[len(home):]
	}
	return p
}

// changes counts porcelain entries; a rename or copy carries its origin as an extra field.
func changes(dir string) int {
	out, _ := git(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	fields := strings.Split(out, "\x00")
	n := 0
	for i := 0; i < len(fields); i++ {
		if fields[i] == "" {
			continue
		}
		n++
		if len(fields[i]) >= 2 && strings.ContainsAny(fields[i][:2], "RC") {
			i++
		}
	}
	return n
}

func inspect(home, p string) Row {
	r := Row{Path: p, display: tilde(home, p), Profile: "-", Email: "aucun"}
	if b, ok := git(p, "symbolic-ref", "--quiet", "--short", "HEAD"); ok {
		r.Branch = b
	} else {
		r.Branch = "détaché"
	}
	r.Changes = changes(p)
	if out, ok := git(p, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); ok {
		fmt.Sscan(out, &r.Ahead, &r.Behind)
	}
	if v, ok := git(p, "config", "dotfiles.profile"); ok && v != "" {
		r.Profile = v
	}
	if v, ok := git(p, "config", "user.email"); ok {
		r.Email = v
	}
	if r.Profile == "-" {
		return r
	}
	pf := filepath.Join(home, ".config", "git", "profiles", r.Profile+".gitconfig")
	if fi, err := os.Stat(pf); err != nil || !fi.Mode().IsRegular() {
		pf = filepath.Join(home, ".config", "git", "profiles", r.Profile+".local")
	}
	if expected, _ := git(p, "config", "-f", pf, "user.email"); expected == "" || r.Email != expected {
		r.Reason = reasonEmail + r.Profile
	}
	if want, _ := git(p, "config", "-f", pf, "--path", "core.hooksPath"); want != "" {
		hooks, _ := git(p, "config", "--path", "core.hooksPath")
		if hooks != want && (hooks != ".githooks" || !executable(filepath.Join(p, ".githooks", "pre-push"))) {
			if r.Reason != "" {
				r.Reason += " ; "
			}
			r.Reason += reasonHooks
		}
	}
	return r
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode()&0o111 != 0
}

// Collect inspects every repository. Git sees the process environment (HOME included).
func Collect(home, src, sandbox string, withSandbox bool) []Row {
	var rows []Row
	for _, p := range Find(src, sandbox, withSandbox) {
		rows = append(rows, inspect(home, p))
	}
	return rows
}

// Format aligns rows into one line each, columns padded to their width in characters.
// The reason is red when color is set.
func Format(rows []Row, color bool) []string {
	cells := make([][6]string, len(rows))
	var w [6]int
	for i, r := range rows {
		cells[i] = [6]string{r.display, r.Branch, fmt.Sprintf("✎%d", r.Changes),
			fmt.Sprintf("⇡%d ⇣%d", r.Ahead, r.Behind), r.Profile, r.Email}
		for j, c := range cells[i] {
			w[j] = max(w[j], utf8.RuneCountInString(c))
		}
	}
	red, reset := "", ""
	if color {
		red, reset = "\033[31m", "\033[0m"
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		var b strings.Builder
		for j, c := range cells[i] {
			b.WriteString(c)
			if j < 5 {
				b.WriteString(strings.Repeat(" ", w[j]-utf8.RuneCountInString(c)+2))
			} else if r.Reason != "" {
				b.WriteString(strings.Repeat(" ", w[j]-utf8.RuneCountInString(c)))
			}
		}
		if r.Reason != "" {
			b.WriteString("  " + red + r.Reason + reset)
		}
		lines[i] = b.String()
	}
	return lines
}

// Problems lists the repositories (src and sandbox) whose identity disagrees with their git
// profile or whose guard hook is bypassed, as aligned uncolored lines ready to print.
// ok is true when there is none. The error is reserved for future failures (always nil today).
func Problems(home, src, sandbox string) (rows []string, ok bool, err error) {
	var bad []Row
	for _, r := range Collect(home, src, sandbox, true) {
		if r.Reason != "" {
			bad = append(bad, r)
		}
	}
	return Format(bad, false), len(bad) == 0, nil
}

// Export writes the unique origin remotes of the repositories under src, sorted bytewise,
// to file (mode 0600, replaced atomically), and returns how many it wrote.
func Export(src, file string) (int, error) {
	set := map[string]bool{}
	for _, p := range Find(src, "", false) {
		if u, ok := git(p, "config", "--get", "remote.origin.url"); ok && u != "" {
			set[u] = true
		}
	}
	urls := make([]string, 0, len(set))
	for u := range set {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	tmp, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	var b strings.Builder
	for _, u := range urls {
		b.WriteString(u + "\n")
	}
	_, werr := tmp.WriteString(b.String())
	if err := errors.Join(werr, tmp.Chmod(0o600), tmp.Close()); err != nil {
		return 0, err
	}
	return len(urls), os.Rename(tmp.Name(), file)
}

// CloneList clones every URL of file under root, continuing after a failure. Git's output is
// never shown (an URL can carry credentials); each failure prints its destination to errw.
func CloneList(file, root string, errw io.Writer) (good, bad int, err error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, 0, ErrUnreadable
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		url := sc.Text()
		if t := strings.TrimLeft(url, " \t\r\f\v"); t == "" || t[0] == '#' {
			continue
		}
		if _, _, err := clone.Clone(url, clone.Options{Root: root, Env: Env()}); err == nil {
			good++
			continue
		}
		bad++
		target, perr := clone.Path(url, root)
		if perr != nil {
			target = "(URL invalide)"
		}
		fmt.Fprintf(errw, "échec : %s\n", target)
	}
	return good, bad, sc.Err()
}
