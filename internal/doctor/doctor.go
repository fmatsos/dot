// Package doctor is the read-only health check behind `dot doctor`: it never writes, fetches or
// reads stdin, and returns structured lines that do not depend on the terminal.
// ponytail: one pass of read-only checks; fixes stay in the other dot commands.
package doctor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fmatsos/dot/internal/guard"
	"github.com/fmatsos/dot/internal/link"
	"github.com/fmatsos/dot/internal/manifest"
	"github.com/fmatsos/dot/internal/repos"
	"github.com/fmatsos/dot/internal/secrets"
)

// Level is the severity of a line. Raw lines carry no symbol (headers, detail rows).
type Level int

const (
	Raw Level = iota
	OK
	Warn
	Fail
)

// Symbol is the marker printed before the message.
func (l Level) Symbol() string { return [...]string{"", "✓", "!", "✗"}[l] }

// Line is one report line.
type Line struct {
	Level   Level
	Message string
}

// Profile is one profile clone to check; Key only names its header.
type Profile struct{ Key, Dir string }

// Options describes one run. Err, when set, is a failure to resolve the targeted profiles
// (empty registry, unknown profile): it is reported first and the machine checks still run.
type Options struct {
	Home         string
	Profiles     []Profile
	Err          error
	Headers      bool // precede each profile with a "profile <key>" line
	SrcRoot      string
	SandboxRoot  string
	StatusBudget time.Duration // secrets status timeout; 20 s when zero
}

// HasFail tells whether any line is a failure (exit code 1).
func HasFail(lines []Line) bool {
	return slices.ContainsFunc(lines, func(l Line) bool { return l.Level == Fail })
}

// Write prints the lines; color wraps the symbol in ANSI colors.
func Write(w io.Writer, lines []Line, color bool) error {
	colors := [...]string{"", "\033[32m", "\033[33m", "\033[31m"}
	for _, l := range lines {
		var err error
		switch {
		case l.Level == Raw:
			_, err = fmt.Fprintln(w, l.Message)
		case color:
			_, err = fmt.Fprintf(w, "%s%s\033[0m %s\n", colors[l.Level], l.Level.Symbol(), l.Message)
		default:
			_, err = fmt.Fprintf(w, "%s %s\n", l.Level.Symbol(), l.Message)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type checker struct {
	o     Options
	lines []Line
}

func (c *checker) add(l Level, format string, a ...any) {
	c.lines = append(c.lines, Line{l, fmt.Sprintf(format, a...)})
}

func (c *checker) raw(s string) { c.lines = append(c.lines, Line{Raw, s}) }

// state is what the checks of one profile share; m is nil when its dot.json is invalid.
type state struct {
	p Profile
	m *manifest.Manifest
}

// Run performs every check. With one profile the order is the historical one; with several,
// each profile is checked in turn and the machine-wide checks run once, after them.
func Run(o Options) []Line {
	o.StatusBudget = cmp.Or(o.StatusBudget, 20*time.Second)
	c := &checker{o: o}
	if o.Err != nil {
		c.add(Fail, "%s", o.Err)
	}
	single := len(o.Profiles) == 1
	sts := make([]*state, len(o.Profiles))
	for i, p := range o.Profiles {
		s := &state{p: p}
		sts[i] = s
		if o.Headers {
			c.raw("profile " + p.Key)
		}
		c.loadManifest(s)
		c.clone(s)
		c.gitProfiles(s)
		c.homeLinks(s)
		if single {
			c.deadLinks()
		}
		c.guard(s)
		if single {
			c.gitInclude()
			c.mise()
			c.shims()
		}
		c.secrets(s)
		c.npx(s)
	}
	if !single {
		c.deadLinks()
		c.gitInclude()
		c.mise()
		c.shims()
	}
	c.marketplaces(sts)
	c.repos()
	return c.lines
}

func (c *checker) loadManifest(s *state) {
	m, err := manifest.Load(filepath.Join(s.p.Dir, "dot.json"))
	if err != nil {
		key := "object"
		var ke *manifest.KeyError
		if errors.As(err, &ke) {
			key = ke.Key
		}
		c.add(Fail, "invalid dot.json: key %s", key)
		c.add(Warn, "profiles and marketplaces skipped: invalid dot.json")
		return
	}
	s.m = m
}

// gitEnv never prompts and never takes optional locks.
func gitEnv() []string { return append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0") }

// git runs git in dir; stdin is /dev/null and stderr is dropped.
func git(dir string, args ...string) (string, bool) {
	return gitOut(append([]string{"-C", dir}, args...)...)
}

func gitOut(args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err == nil
}

// gitFile reads one key of a gitconfig file; empty when the file or the key is missing.
func gitFile(file, key string) string {
	v, _ := gitOut("config", "-f", file, key)
	return v
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// profileFile is where the bash doctor looked for a git profile: the copy linked in ~.
func (c *checker) profileFile(path string) string {
	return filepath.Join(c.o.Home, strings.TrimPrefix(path, "home/"))
}

func (c *checker) clone(s *state) {
	dir := s.p.Dir
	if _, ok := git(dir, "rev-parse", "--git-dir"); !isDir(filepath.Join(dir, ".git")) || !ok {
		c.add(Fail, "deployed clone missing: rerun install.sh")
		return
	}
	c.add(OK, "deployed clone present")
	if h, _ := git(dir, "config", "--local", "core.hooksPath"); h == ".githooks" {
		c.add(OK, "deployed clone hooks")
	} else {
		c.add(Fail, "clone hooks: rerun install.sh")
	}
	if s.m != nil {
		name := s.m.DeployProfile
		var expected string
		for _, pp := range s.m.Profiles {
			if pp.Name == name {
				expected = gitFile(c.profileFile(pp.Path), "user.email")
			}
		}
		if got, _ := git(dir, "config", "user.email"); expected != "" && got == expected {
			c.add(OK, "clone email: profile %s", name)
		} else {
			c.add(Fail, "clone email: restore profile %s (install.sh)", name)
		}
	}
	if st, _ := git(dir, "status", "--porcelain=v1", "--untracked-files=all"); st != "" {
		c.add(Warn, "clone modified: dot push")
	} else {
		c.add(OK, "clone has no changes")
	}
	counts, ok := git(dir, "rev-list", "--left-right", "--count", "HEAD...refs/remotes/origin/HEAD")
	if !ok {
		counts, ok = git(dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	}
	var ahead, behind int
	if ok {
		fmt.Sscan(counts, &ahead, &behind)
	}
	if ahead > 0 || behind > 0 {
		c.add(Warn, "clone: ⇡%d ⇣%d — dot push / dot pull", ahead, behind)
	} else {
		c.add(OK, "clone neither ahead nor behind, as far as known")
	}
}

func (c *checker) gitProfiles(s *state) {
	if s.m == nil {
		return
	}
	for _, pp := range s.m.Profiles {
		f := c.profileFile(pp.Path)
		if fi, err := os.Stat(f); err == nil && fi.Mode().IsRegular() && gitFile(f, "user.email") != "" {
			c.add(OK, "profile %s", pp.Name)
		} else {
			c.add(Fail, "profile %s: file missing or user.email missing", pp.Name)
		}
	}
}

func (c *checker) homeLinks(s *state) {
	if !isDir(filepath.Join(s.p.Dir, "home")) {
		c.add(Fail, "clone home missing: rerun install.sh")
		return
	}
	d := link.Detached(s.p.Dir, c.o.Home)
	if len(d) == 0 {
		c.add(OK, "home links up to date")
		return
	}
	c.add(Fail, "%d detached files: rerun install.sh after reconciling", len(d))
	for _, p := range d[:min(len(d), 10)] {
		c.raw("  " + p)
	}
}

func (c *checker) deadLinks() {
	if n := link.DeadLinks(c.o.Home); n == 0 {
		c.add(OK, "no dead links in ~/.local/bin")
	} else {
		c.add(Fail, "%d dead links in ~/.local/bin: rerun install.sh", n)
	}
}

// guard loads the forbidden list with the guard's own validation, never showing a line of it.
func (c *checker) guard(s *state) {
	_, err := guard.LoadTerms(cmp.Or(os.Getenv("DOTFILES_FORBIDDEN"), filepath.Join(s.p.Dir, "forbidden.local")))
	switch {
	case err == nil:
		c.add(OK, "guard enabled")
	case errors.Is(err, guard.ErrInvalid):
		c.add(Fail, "guard disabled: invalid terms list, fix forbidden.local")
	default:
		c.add(Fail, "guard disabled: restore forbidden.local")
	}
}

// npx checks that a profile asking for the skills module can run the skills CLI.
func (c *checker) npx(s *state) {
	if s.m == nil || !slices.Contains(s.m.Modules, "skills") {
		return
	}
	if _, err := exec.LookPath("npx"); err != nil {
		c.add(Fail, "skills module: npx not found, install Node (nvm key of dot.json)")
		return
	}
	c.add(OK, "npx found for the skills module")
}

func (c *checker) gitInclude() {
	out, _ := gitOut("config", "--global", "--get-all", "include.path")
	for _, p := range strings.Split(out, "\n") {
		if p == "~/.config/git/profiles.gitconfig" || p == filepath.Join(c.o.Home, ".config", "git", "profiles.gitconfig") {
			c.add(OK, "profiles included in the global git config")
			return
		}
	}
	c.add(Fail, "profiles not included: rerun install.sh")
}

// findMise returns the mise binary the machine has: the PATH one, else ~/.local/bin/mise.
func (c *checker) findMise() string {
	if mise, _ := exec.LookPath("mise"); mise != "" {
		return mise
	}
	if p := filepath.Join(c.o.Home, ".local", "bin", "mise"); isExec(p) {
		return p
	}
	return ""
}

// wantMise tells whether the machine is expected to have mise: the installer only installs it when
// a targeted profile ships ~/.config/mise/config.toml, so without one (and without a mise already
// there) its absence is not a finding and the mise checks stay silent.
func (c *checker) wantMise() bool {
	for _, p := range c.o.Profiles {
		if fi, err := os.Stat(filepath.Join(p.Dir, "home", ".config", "mise", "config.toml")); err == nil && fi.Mode().IsRegular() {
			return true
		}
	}
	return c.findMise() != ""
}

func (c *checker) mise() {
	if !c.wantMise() {
		return
	}
	mise := c.findMise()
	if mise == "" {
		c.add(Fail, "mise missing: rerun install.sh")
		return
	}
	c.add(OK, "mise present")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, mise, "ls", "--missing", "--no-header", "--no-truncate")
	cmd.Env = append(os.Environ(), "MISE_OFFLINE=true", "NO_COLOR=1")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		c.add(Fail, "mise tools unverifiable: check mise ls --missing")
		return
	}
	var tools []string
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			tools = append(tools, f[0])
		}
	}
	slices.Sort(tools)
	if tools = slices.Compact(tools); len(tools) == 0 {
		c.add(OK, "mise tools installed")
	} else {
		c.add(Fail, "mise tools missing: %s — mise install", strings.Join(tools, ","))
	}
}

func (c *checker) shims() {
	if !c.wantMise() {
		return
	}
	data := cmp.Or(os.Getenv("MISE_DATA_DIR"),
		filepath.Join(cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(c.o.Home, ".local", "share")), "mise"))
	if slices.Contains(filepath.SplitList(os.Getenv("PATH")), filepath.Join(data, "shims")) {
		c.add(OK, "mise shims in PATH")
	} else {
		c.add(Warn, "mise shims not in PATH: load ~/.config/mise/shell.sh")
	}
}

// secrets reports counts only, within a time budget for the vault commands.
func (c *checker) secrets(s *state) {
	if _, err := os.Stat(filepath.Join(s.p.Dir, secrets.File)); err != nil {
		c.add(OK, "no secrets declared")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.o.StatusBudget)
	defer cancel()
	entries, err := secrets.Status(ctx, s.p.Dir)
	var readable, locked, unavailable int
	for _, e := range entries {
		switch e.State {
		case secrets.Readable:
			readable++
		case secrets.Locked:
			locked++
		default:
			unavailable++
		}
	}
	msg := fmt.Sprintf("secrets: %d readable / %d locked / %d unavailable", readable, locked, unavailable)
	if err == nil && locked == 0 && unavailable == 0 {
		c.add(OK, "%s", msg)
	} else if err != nil {
		// Status fails only on the mapping file itself (malformed, duplicate NAME): unlocking cannot repair it.
		c.add(Fail, "invalid secrets.local (%v): fix the file", err)
	} else {
		c.add(Warn, "%s (incomplete status or blocked access): dot secrets unlock", msg)
	}
}

func (c *checker) repos() {
	rows, ok, err := repos.Problems(c.o.Home, c.o.SrcRoot, c.o.SandboxRoot)
	if err == nil && ok {
		c.add(OK, "repository identities consistent")
		return
	}
	c.add(Fail, "repository identities inconsistent: dot profile / git config user.email")
	for _, r := range rows {
		c.raw(r)
	}
}
