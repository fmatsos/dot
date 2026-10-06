// Package link manages the symlinks from a profile clone into ~: planning, applying with
// backups, drift and dead-link checks, conflicts between profiles, and uninstalling.
// It never touches the terminal: messages go to the writers it is given.
package link

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fmatsos/dot/internal/registry"
)

// Link is one symlink to create: Dst (absolute, under home) points to Src (absolute, in a profile).
type Link struct {
	Src, Dst string
	Layer    string // home layer providing Src ("home", "home@darwin"…); empty for bin/
}

// NoHomeError reports a profile without a home/ directory (dry run without clone).
type NoHomeError struct{ Dir string }

func (e *NoHomeError) Error() string {
	return fmt.Sprintf("rien à lier : %s/home absent (dry run sans clone ?)", e.Dir)
}

// ModuleSources are the home/ files that `dot settings` and `dot mcp` merge across profiles. With
// two or more registered profiles they are per-profile inputs and are not linked into ~; with one,
// they are linked like any other file.
var ModuleSources = []string{".claude/settings.base.json", ".config/mcp/servers.json"}

// Tilde shortens a path under home to ~/…, like the bash `${p/#$HOME/~}`.
func Tilde(home, p string) string {
	if rest, ok := strings.CutPrefix(p, home); ok {
		return "~" + rest
	}
	return p
}

// files lists the regular files under root, sorted; a missing root gives none.
func files(root string, keep func(fs.DirEntry) bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && keep(d) {
			out = append(out, p)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return out, err
}

// homeLinks lists the home links of a profile from the layers active here; multi leaves out the ModuleSources.
func homeLinks(dir, home string, multi bool) ([]Link, error) {
	return layerLinks(dir, home, activeLayers(), multi)
}

// binLinks: executable files directly in dir/bin, linked into ~/.local/bin.
func binLinks(dir, home string) ([]Link, error) {
	root := filepath.Join(dir, "bin")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var links []Link
	for _, e := range entries {
		if i, ierr := e.Info(); ierr == nil && i.Mode().IsRegular() && i.Mode().Perm()&0o100 != 0 {
			links = append(links, Link{Src: filepath.Join(root, e.Name()), Dst: filepath.Join(home, ".local", "bin", e.Name())})
		}
	}
	return links, err
}

// Plan lists every link a profile wants (home/** file by file, then bin/ executables), sorted by Dst.
func Plan(dir, home string) ([]Link, error) { return PlanFor(dir, home, false) }

// PlanFor is Plan for a run with several registered profiles when multi: the ModuleSources are not planned.
func PlanFor(dir, home string, multi bool) ([]Link, error) {
	dir, _ = filepath.Abs(dir)
	h, err := homeLinks(dir, home, multi)
	if err != nil {
		return nil, err
	}
	b, err := binLinks(dir, home)
	if err != nil {
		return nil, err
	}
	all := append(h, b...)
	slices.SortStableFunc(all, func(x, y Link) int { return strings.Compare(x.Dst, y.Dst) })
	return all, nil
}

// Linker applies plans with one backup directory for the whole run.
type Linker struct {
	Home   string
	Dry    bool
	Out    io.Writer // progress lines (stdout in bash)
	Err    io.Writer // warnings (stderr in bash)
	Backup string    // ~/.local/state/dotfiles/backup/<stamp>, fixed at New
	// Registered lists the clone of every registered profile. With two or more, Apply skips the
	// ModuleSources and removes an existing link to one of them from these clones.
	Registered []string
}

// New fixes the backup directory from now(); a nil writer discards.
func New(home string, dry bool, out, errOut io.Writer, now func() time.Time) *Linker {
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}
	return &Linker{Home: home, Dry: dry, Out: out, Err: errOut, Backup: filepath.Join(BackupBase(home), now().Format(stampLayout))}
}

const stampLayout = "20060102-150405"

// BackupBase is the directory holding the timestamped backup directories.
func BackupBase(home string) string {
	return filepath.Join(home, ".local", "state", "dotfiles", "backup")
}

// run mirrors the bash `run`: print in dry mode, else do.
func (l *Linker) run(desc string, do func() error) error {
	if l.Dry {
		_, err := fmt.Fprintf(l.Out, "  [dry] %s\n", desc)
		return err
	}
	return do()
}

func (l *Linker) mkdirAll(d string) error {
	return l.run("mkdir -p "+d, func() error { return os.MkdirAll(d, 0o755) })
}

// Apply links one profile: home files, dead bin links left by removed scripts, then bin/ itself.
// It returns *NoHomeError, without touching anything, when dir/home is absent.
func (l *Linker) Apply(dir string) error {
	dir, _ = filepath.Abs(dir)
	if i, err := os.Stat(filepath.Join(dir, "home")); err != nil || !i.IsDir() {
		return &NoHomeError{dir}
	}
	multi := len(l.Registered) > 1
	hl, err := homeLinks(dir, l.Home, multi)
	if err != nil {
		return err
	}
	bl, err := binLinks(dir, l.Home)
	if err != nil {
		return err
	}
	if multi {
		if err := l.dropModuleLinks(); err != nil {
			return err
		}
	}
	for _, k := range hl {
		if err := l.one(dir, k); err != nil {
			return err
		}
	}
	if err := l.orphanLinks(dir, append(hl[:len(hl):len(hl)], bl...)); err != nil {
		return err
	}
	if err := l.deadLinks(dir); err != nil {
		return err
	}
	for _, k := range bl {
		if err := l.one(dir, k); err != nil {
			return err
		}
	}
	return nil
}

// dropModuleLinks removes the links to a ModuleSource of a registered profile left by an install
// that had a single profile: the modules then merge the profiles instead of reading one of them.
func (l *Linker) dropModuleLinks() error {
	for _, rel := range ModuleSources {
		dst := filepath.Join(l.Home, rel)
		t, err := os.Readlink(dst)
		if err != nil {
			continue
		}
		for _, d := range l.Registered {
			if abs, _ := filepath.Abs(d); t != filepath.Join(abs, "home", rel) {
				continue
			}
			fmt.Fprintf(l.Out, "  lien retiré : %s\n", Tilde(l.Home, dst))
			if err := l.run("rm -f "+dst, func() error { return os.Remove(dst) }); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

// deadLinks removes broken links of ~/.local/bin that pointed into dir/bin/.
func (l *Linker) deadLinks(dir string) error {
	bin := filepath.Join(l.Home, ".local", "bin")
	entries, _ := os.ReadDir(bin) // sorted; like the bash glob, hidden names are skipped
	for _, e := range entries {
		p := filepath.Join(bin, e.Name())
		if strings.HasPrefix(e.Name(), ".") || e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if t, _ := os.Readlink(p); !strings.HasPrefix(t, dir+"/bin/") {
			continue
		}
		fmt.Fprintf(l.Out, "  lien mort retiré : %s\n", Tilde(l.Home, p))
		if err := l.run("rm -f "+p, func() error { return os.Remove(p) }); err != nil {
			return err
		}
	}
	return nil
}

// one links a single file, moving whatever sits at the target into the run's backup.
func (l *Linker) one(dir string, k Link) error {
	t, rerr := os.Readlink(k.Dst)
	if rerr == nil && t == k.Src {
		return nil
	}
	if rerr == nil && inProfile(t, dir) {
		// A dot link to another layer or a moved file: re-point it, there is nothing to back up.
		fmt.Fprintf(l.Out, "  lien : %s%s\n", Tilde(l.Home, k.Dst), layerNote(k))
		if err := l.run("rm -f "+k.Dst, func() error { return os.Remove(k.Dst) }); err != nil {
			return err
		}
		return l.run("ln -s "+k.Src+" "+k.Dst, func() error { return os.Symlink(k.Src, k.Dst) })
	}
	fi, err := os.Lstat(k.Dst)
	if err == nil && fi.IsDir() {
		fmt.Fprintf(l.Err, "  ignoré (dossier existant) : %s\n", k.Dst)
		return nil
	}
	if err == nil {
		// Reached through a directory link into the profile: Dst is Src, moving it would lose the source.
		if a, e1 := os.Stat(k.Src); e1 == nil {
			if b, e2 := os.Stat(k.Dst); e2 == nil && os.SameFile(a, b) {
				return nil
			}
		}
		if !(fi.Mode().IsRegular() && sameContent(k.Src, k.Dst)) {
			fmt.Fprintf(l.Out, "  sauvegarde : %s → %s/\n", Tilde(l.Home, k.Dst), Tilde(l.Home, l.Backup))
		}
		rel, _ := filepath.Rel(l.Home, k.Dst)
		to := filepath.Join(l.Backup, rel)
		if err := l.mkdirAll(filepath.Dir(to)); err != nil {
			return err
		}
		if err := l.run("mv "+k.Dst+" "+to, func() error { return os.Rename(k.Dst, to) }); err != nil {
			return err
		}
	}
	fmt.Fprintf(l.Out, "  lien : %s%s\n", Tilde(l.Home, k.Dst), layerNote(k))
	if err := l.mkdirAll(filepath.Dir(k.Dst)); err != nil {
		return err
	}
	return l.run("ln -s "+k.Src+" "+k.Dst, func() error { return os.Symlink(k.Src, k.Dst) })
}

// layerNote names the winning layer of a link when it is not home/.
func layerNote(k Link) string {
	if k.Layer == "" || k.Layer == "home" {
		return ""
	}
	return " (" + k.Layer + ")"
}

// sameContent is `cmp -s`. ponytail: reads both files whole; fine for dotfiles, not for large blobs.
func sameContent(a, b string) bool {
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// Detached lists the home/ files of dir whose ~ twin is not a link to them, as ~/… paths.
// With several registered profiles the ModuleSources are not expected to be linked.
func Detached(dir, home string) []string {
	dir, _ = filepath.Abs(dir)
	links, _ := homeLinks(dir, home, multiProfile(home))
	var out []string
	for _, k := range links {
		if t, err := os.Readlink(k.Dst); err != nil || t != k.Src {
			out = append(out, Tilde(home, k.Dst))
		}
	}
	return out
}

// multiProfile tells whether two or more profiles are registered; an unreadable registry counts as one.
func multiProfile(home string) bool {
	r, err := registry.Load(filepath.Join(home, ".dot", "profiles.json"))
	return err == nil && len(r.Keys()) > 1
}

// Drift is Detached formatted as the report lines of `dot status`.
func Drift(dir, home string) []string {
	d := Detached(dir, home)
	for i, p := range d {
		d[i] = "détaché (plus un lien, à réconcilier) : " + p
	}
	return d
}

// DeadLinks counts the broken symlinks directly in ~/.local/bin.
func DeadLinks(home string) int {
	bin := filepath.Join(home, ".local", "bin")
	entries, _ := os.ReadDir(bin)
	n := 0
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		if _, err := os.Stat(filepath.Join(bin, e.Name())); err != nil {
			n++
		}
	}
	return n
}

// removeEmptyDir removes d only if it is a real, empty directory: a symlink to a directory
// (~/.config pointing elsewhere) is the user's, and os.Remove would unlink it.
func removeEmptyDir(d string) bool {
	fi, err := os.Lstat(d)
	return err == nil && fi.IsDir() && os.Remove(d) == nil
}

// target reads the symlink p as a clean absolute path.
func target(p string) (string, bool) {
	t, err := os.Readlink(p)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(filepath.Dir(p), t)
	}
	return filepath.Clean(t), true
}

// symlinkCandidates lists the symlinks that may be dot links: the planned destinations, the
// symlinks directly in ~ and ~/.local/bin and, recursively without ever entering a symlink, in
// every top-level directory of ~ the plan touches. ponytail: an orphan link outside the root
// directories the profile manages is not found.
func symlinkCandidates(plan []Link, home string) []string {
	var cands, roots []string
	for _, k := range plan {
		cands = append(cands, k.Dst)
		rel, err := filepath.Rel(home, k.Dst)
		if top, _, nested := strings.Cut(rel, string(filepath.Separator)); err == nil && nested {
			if fi, err := os.Lstat(filepath.Join(home, top)); err == nil && fi.IsDir() {
				roots = append(roots, filepath.Join(home, top))
			}
		}
	}
	for _, d := range []string{home, filepath.Join(home, ".local", "bin")} {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if e.Type()&fs.ModeSymlink != 0 {
				cands = append(cands, filepath.Join(d, e.Name()))
			}
		}
	}
	slices.Sort(roots)
	for _, r := range slices.Compact(roots) {
		// WalkDir never follows symlinks; an unreadable subdirectory is skipped, not fatal.
		_ = filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type()&fs.ModeSymlink != 0 {
				cands = append(cands, p)
			}
			return nil
		})
	}
	slices.Sort(cands)
	return slices.Compact(cands)
}

// orphanLinks removes the links into a home layer of dir that the plan no longer wants: broken
// ones (a file deleted from the last layer providing it) and those into a layer that is not
// active here (a host name that changed). A path still provided by an active layer is re-pointed by one().
func (l *Linker) orphanLinks(dir string, plan []Link) error {
	planned := map[string]bool{}
	for _, k := range plan {
		planned[k.Dst] = true
	}
	for _, p := range symlinkCandidates(plan, l.Home) {
		t, ok := target(p)
		if !ok || planned[p] || !inProfile(t, dir) || strings.HasPrefix(t, dir+"/bin/") {
			continue
		}
		_, serr := os.Stat(p)
		rel, _ := filepath.Rel(dir, t)
		top, _, _ := strings.Cut(rel, string(filepath.Separator))
		if serr == nil && slices.Contains(activeLayers(), top) {
			continue // still provided here (a module source in multi mode is dropped elsewhere)
		}
		msg := "lien retiré"
		if serr != nil {
			msg = "lien mort retiré"
		}
		fmt.Fprintf(l.Out, "  %s : %s\n", msg, Tilde(l.Home, p))
		if err := l.run("rm -f "+p, func() error { return os.Remove(p) }); err != nil {
			return err
		}
	}
	return nil
}

// Unlink removes the links of home that point into a home layer (active or not) or bin/ of dir
// (nothing else, backups stay) and the parent directories they leave empty, home excluded.
// Besides the planned destinations it looks at the symlinks directly in ~ and ~/.local/bin and in
// the top-level directories of ~ the plan touches, so a link to a file deleted from the profile
// since the install goes too, even from a folder the plan lost.
func Unlink(dir, home string, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	dir, _ = filepath.Abs(dir)
	plan, err := layerLinks(dir, home, allLayers(dir), false)
	if err != nil {
		return err
	}
	b, err := binLinks(dir, home)
	if err != nil {
		return err
	}
	for _, p := range symlinkCandidates(append(plan, b...), home) {
		if t, ok := target(p); !ok || !inProfile(t, dir) {
			continue
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		fmt.Fprintf(out, "  lien retiré : %s\n", Tilde(home, p))
		for d := filepath.Dir(p); strings.HasPrefix(d, home+"/") && removeEmptyDir(d); d = filepath.Dir(d) {
		}
	}
	return nil
}
