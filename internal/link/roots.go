package link

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// planRoots lists the top-level directories of home (~/.config…) the plan links into.
func planRoots(plan []Link, home string) []string {
	var roots []string
	for _, k := range plan {
		rel, err := filepath.Rel(home, k.Dst)
		if top, _, nested := strings.Cut(rel, string(filepath.Separator)); err == nil && nested {
			if fi, err := os.Lstat(filepath.Join(home, top)); err == nil && fi.IsDir() {
				roots = append(roots, filepath.Join(home, top))
			}
		}
	}
	slices.Sort(roots)
	return slices.Compact(roots)
}

// rootsFile remembers the roots of the last install of the profile in dir, so a later pass still
// scans a root whose last source was deleted since. ponytail: filled from the first install that
// ran with it; a root lost before then is not found.
func rootsFile(home, dir string) string {
	return filepath.Join(home, ".local", "state", "dotfiles", "roots", filepath.Base(dir))
}

// savedRoots reads the roots remembered for dir; a missing file is an empty list.
func savedRoots(home, dir string) []string {
	data, _ := os.ReadFile(rootsFile(home, dir))
	var roots []string
	for _, r := range strings.Split(string(data), "\n") { // one path per line: a path may hold spaces
		if filepath.Dir(r) == home { // never a path outside the top level of home
			roots = append(roots, r)
		}
	}
	return roots
}

// saveRoots replaces the remembered roots by those of plan (nothing is written in a dry run).
func (l *Linker) saveRoots(dir string, plan []Link) error {
	if l.Dry {
		return nil
	}
	f := rootsFile(l.Home, dir)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(strings.Join(planRoots(plan, l.Home), "\n")+"\n"), 0o644)
}
