// Package install deploys profiles: partial clone, git identity, links in ~, backup purge,
// tools and modules. It never reads the terminal; messages go to the writers it is given.
package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fmatsos/dot/internal/link"
	"github.com/fmatsos/dot/internal/manifest"
	"github.com/fmatsos/dot/internal/mcp"
	"github.com/fmatsos/dot/internal/registry"
	"github.com/fmatsos/dot/internal/settings"
)

// Profile is a profile clone: Key is its registry key, Dir its absolute directory.
type Profile struct{ Key, Dir string }

// ToolProfile is a profile being installed, with its validated manifest.
type ToolProfile struct {
	Key, Dir string
	Manifest *manifest.Manifest
}

// ToolsRequest is what the Tools hook receives: the profiles of this run, once their links are
// made. Skip is $DOTFILES_TOOLS=0 (the pinned downloads may still be checked, never run).
type ToolsRequest struct {
	Home     string
	Dry      bool
	Skip     bool
	Profiles []ToolProfile
	Out, Err io.Writer
}

// ErrReported marks a failure the Installer already printed (modules): the caller only sets the exit code.
var ErrReported = errors.New("échec déjà signalé")

const includePath = "~/.config/git/profiles.gitconfig" // literal value: git expands it itself

// Installer runs the installation steps. Zero values are completed by New.
type Installer struct {
	Home     string
	Dry      bool
	Out, Err io.Writer
	Getenv   func(string) string
	Now      func() time.Time
	Tools    func(ToolsRequest) error // nil: no tools (mise, nvm, plugins)
}

// New returns an Installer with the process environment and clock.
func New(home string, dry bool, out, errOut io.Writer) *Installer {
	return &Installer{Home: home, Dry: dry, Out: out, Err: errOut, Getenv: os.Getenv, Now: time.Now}
}

func (in *Installer) regPath() string         { return filepath.Join(in.Home, ".dot", "profiles.json") }
func (in *Installer) dirOf(key string) string { return filepath.Join(in.Home, ".dot", key) }

// Registered lists the registered profiles in registry order.
func (in *Installer) Registered() ([]Profile, error) {
	r, err := registry.Load(in.regPath())
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, k := range r.Keys() {
		out = append(out, Profile{k, in.dirOf(k)})
	}
	return out, nil
}

// all is every registered profile plus the batch members the registry does not know yet.
func (in *Installer) all(batch []Profile) ([]Profile, error) {
	all, err := in.Registered()
	if err != nil {
		return nil, err
	}
	for _, p := range batch {
		if !slices.ContainsFunc(all, func(q Profile) bool { return q.Dir == p.Dir }) {
			all = append(all, p)
		}
	}
	return all, nil
}

// loaded is a profile ready to install: manifest read, identity found.
type loaded struct {
	Profile
	M           *manifest.Manifest
	Name, Email string
}

// load reads the manifest and the git identity of the profile named by deployProfile.
func (in *Installer) load(p Profile) (loaded, error) {
	m, err := manifest.Load(filepath.Join(p.Dir, "dot.json"))
	if err != nil {
		return loaded{}, err
	}
	l := loaded{Profile: p, M: m}
	for _, pp := range m.Profiles {
		if pp.Name != m.DeployProfile {
			continue
		}
		file := filepath.Join(p.Dir, pp.Path)
		l.Name, _ = gitText("config", "-f", file, "user.name")
		l.Email, _ = gitText("config", "-f", file, "user.email")
		if l.Name == "" || l.Email == "" {
			return loaded{}, fmt.Errorf("profil %s : user.name et user.email requis (%s)", m.DeployProfile, file)
		}
	}
	return l, nil
}

// run mirrors the bash `run` for git on a clone: print in dry mode, else do.
func (in *Installer) run(dir string, args ...string) error {
	if in.Dry {
		_, err := fmt.Fprintf(in.Out, "  [dry] git -C %s %s\n", dir, strings.Join(args, " "))
		return err
	}
	return git(dir, args...)
}

func (in *Installer) header(batch []Profile, p Profile) {
	if len(batch) > 1 {
		fmt.Fprintf(in.Out, "==> %s\n", p.Key)
	}
}

// Reinstall runs the installation steps for batch; conflicts are checked against every
// registered profile. Without keepGoing the first failing profile stops the run.
func (in *Installer) Reinstall(batch []Profile) error {
	all, err := in.all(batch)
	if err != nil {
		return err
	}
	_, err = in.install(batch, all, false, nil)
	return err
}

// install is the common sequence. Reads and conflict checks come first, so a refusal writes
// nothing; commit (optional) runs right after them, then per profile identity and links; then the shared steps once. With lenient, a
// profile failing before the conflict check or in its own steps is reported and skipped.
func (in *Installer) install(batch, all []Profile, lenient bool, commit func() error) (failed []string, err error) {
	fail := func(p Profile, e error) error {
		if !lenient {
			return e
		}
		fmt.Fprintf(in.Err, "install %s : %v\n", p.Key, e)
		failed = append(failed, p.Key)
		return nil
	}
	var ls []loaded
	for _, p := range batch {
		l, e := in.load(p)
		if e == nil {
			ls = append(ls, l)
		} else if e = fail(p, e); e != nil {
			return failed, e
		}
	}
	lp := make([]link.Profile, len(all))
	for i, p := range all {
		lp[i] = link.Profile{Key: p.Key, Dir: p.Dir}
	}
	if _, err := link.Conflicts(in.Home, lp); err != nil {
		return failed, err
	}
	if commit != nil {
		if err := commit(); err != nil {
			return failed, err
		}
	}
	lk := link.New(in.Home, in.Dry, in.Out, in.Err, in.Now)
	var done []loaded
	for _, l := range ls {
		in.header(batch, l.Profile)
		if e := in.profileSteps(lk, l); e != nil {
			if e = fail(l.Profile, e); e != nil {
				return failed, e
			}
			continue
		}
		done = append(done, l)
	}
	if len(done) > 0 {
		if err := in.shared(done, all); err != nil {
			return failed, err
		}
	}
	if len(failed) == 0 {
		if in.Dry {
			fmt.Fprintln(in.Out, "ok (dry run)")
		} else {
			fmt.Fprintln(in.Out, "ok")
		}
	}
	return failed, nil
}

// profileSteps: git identity of the clone, hooks path, links. A profile without home/ only warns.
func (in *Installer) profileSteps(lk *link.Linker, l loaded) error {
	for _, kv := range [][2]string{{"user.name", l.Name}, {"user.email", l.Email}, {"core.hooksPath", ".githooks"}} {
		if err := in.run(l.Dir, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	var nh *link.NoHomeError
	if err := lk.Apply(l.Dir); errors.As(err, &nh) {
		fmt.Fprintln(in.Out, nh)
	} else if err != nil {
		return err
	}
	return nil
}

// shared runs what is done once per run: backup purge, include.path, tools, modules.
func (in *Installer) shared(done []loaded, all []Profile) error {
	if err := link.PurgeBackupsFrom(in.Home, in.Getenv("DOTFILES_BACKUP_DAYS"), in.Dry, in.Out, in.Err, in.Now); err != nil {
		return err
	}
	if cur, _ := gitText("config", "--global", "--get-all", "include.path"); !slices.Contains(strings.Split(cur, "\n"), includePath) {
		if in.Dry {
			fmt.Fprintf(in.Out, "  [dry] git config --global --add include.path %s\n", includePath)
		} else if err := git("", "config", "--global", "--add", "include.path", includePath); err != nil {
			return err
		}
	}
	if in.Tools != nil {
		req := ToolsRequest{Home: in.Home, Dry: in.Dry, Skip: in.Getenv("DOTFILES_TOOLS") == "0", Out: in.Out, Err: in.Err}
		for _, l := range done {
			req.Profiles = append(req.Profiles, ToolProfile{l.Key, l.Dir, l.M})
		}
		if err := in.Tools(req); err != nil {
			return err
		}
	}
	return in.modules(done, all)
}

// modules runs the settings and mcp modules declared by any profile of the run. Their sources
// are every registered profile, in registry order, like `dot settings` and `dot mcp` without -p.
func (in *Installer) modules(done []loaded, all []Profile) error {
	var names []string
	for _, l := range done {
		for _, m := range l.M.Modules {
			if !slices.Contains(names, m) {
				names = append(names, m)
			}
		}
	}
	multi := len(all) > 1
	for _, name := range names {
		var err error
		switch name {
		case "settings":
			o := settings.Options{Home: in.Home, SkipMissing: multi, DryRun: in.Dry, Out: in.Out}
			for _, p := range all {
				o.Bases = append(o.Bases, filepath.Join(p.Dir, "home", ".claude", "settings.base.json"))
			}
			err = settings.Run(o)
		case "mcp":
			o := mcp.Options{Home: in.Home, FallbackOnApply: multi, DryRun: in.Dry, Out: in.Out}
			for _, p := range all {
				o.Fallback = append(o.Fallback, filepath.Join(p.Dir, "home", ".config", "mcp", "servers.json"))
				o.SecretsFiles = append(o.SecretsFiles, filepath.Join(p.Dir, "secrets.local"))
			}
			err = mcp.Run(o)
		}
		if err != nil {
			fmt.Fprintf(in.Err, "%s : %v\n", name, err)
			return ErrReported
		}
	}
	return nil
}

// Pull updates every profile of batch (git pull --rebase), then installs the updated ones.
// A profile that fails is reported with its name and skipped; the error is returned at the end.
func (in *Installer) Pull(batch []Profile) error {
	var ok []Profile
	var failed []string
	for _, p := range batch {
		in.header(batch, p)
		if err := pull(p.Dir, in.Out, in.Err); err != nil {
			fmt.Fprintf(in.Err, "pull %s : %v\n", p.Key, err)
			failed = append(failed, p.Key)
			continue
		}
		ok = append(ok, p)
	}
	if len(ok) > 0 {
		all, err := in.all(ok)
		if err != nil {
			return err
		}
		f, err := in.install(ok, all, true, nil)
		if err != nil {
			return err
		}
		failed = append(failed, f...)
	}
	if len(failed) > 0 {
		fmt.Fprintf(in.Err, "échec : %s\n", strings.Join(failed, ", "))
		return ErrReported
	}
	return nil
}
