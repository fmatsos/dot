package link

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// BackupRun is one backup directory: BackupBase/<stamp>/, holding the replaced files at their
// path relative to ~.
type BackupRun struct {
	ID    string   // directory name, YYYYMMDD-HHMMSS
	Files []string // slash-separated paths relative to ~, sorted
}

// ListBackups returns the backup runs, most recent first. Symlinked runs and a symlinked base
// are ignored, like in PurgeBackups. No base means no runs.
func ListBackups(home string) ([]BackupRun, error) {
	base := BackupBase(home)
	if i, err := os.Lstat(base); err != nil || !i.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var runs []BackupRun
	for _, e := range entries {
		if !e.IsDir() || !stampRe.MatchString(e.Name()) { // IsDir is false for symlinks
			continue
		}
		files, err := RunFiles(home, e.Name())
		if err != nil {
			return nil, err
		}
		runs = append(runs, BackupRun{ID: e.Name(), Files: files})
	}
	slices.SortFunc(runs, func(a, b BackupRun) int { return strings.Compare(b.ID, a.ID) })
	return runs, nil
}

// runDir validates a run id (a bare directory name, never a path) and returns its directory,
// which must be a real directory, not a link.
func runDir(home, id string) (string, error) {
	if id == "" || id == "." || id == ".." || id != filepath.Base(id) || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("identifiant de sauvegarde invalide : %q", id)
	}
	if i, err := os.Lstat(BackupBase(home)); err != nil || !i.IsDir() {
		return "", fmt.Errorf("sauvegarde introuvable : %s", id)
	}
	d := filepath.Join(BackupBase(home), id)
	if i, err := os.Lstat(d); err != nil || !i.IsDir() {
		return "", fmt.Errorf("sauvegarde introuvable : %s", id)
	}
	return d, nil
}

// RunFiles lists the backed-up paths of a run, relative to ~. Symlinks inside a backup are listed
// as entries and never followed.
func RunFiles(home, id string) ([]string, error) {
	d, err := runDir(home, id)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() {
			rel, _ := filepath.Rel(d, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// RestoreRequest asks to put back files of one backup run.
type RestoreRequest struct {
	Home   string
	Run    string   // run id
	Paths  []string // files or directories to restore; empty means the whole run
	Clones []string // clone directories of the registered profiles
	Dry    bool
	Out    io.Writer
	Err    io.Writer
}

// Restore moves backed-up files back into ~. A target is only taken when it is absent or a link into
// a home layer (home/, home@*/) or dir/bin/ of a registered clone (a dot link, removed first); any other file, or a
// target below a linked directory, is refused and reported on Err, and the run goes on. It returns
// the counts of restored and refused files. The run directory goes away once it is empty.
func Restore(r RestoreRequest) (restored, refused int, err error) {
	out, errOut := r.Out, r.Err
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}
	run, err := runDir(r.Home, r.Run)
	if err != nil {
		return 0, 0, err
	}
	all, err := RunFiles(r.Home, r.Run)
	if err != nil {
		return 0, 0, err
	}
	sel := all
	if len(r.Paths) > 0 {
		sel = nil
		for _, p := range r.Paths {
			rel, perr := backupRel(r.Home, run, p)
			if perr != nil {
				return 0, 0, perr
			}
			n := 0
			for _, f := range all {
				if f == rel || strings.HasPrefix(f, rel+"/") {
					sel = append(sel, f)
					n++
				}
			}
			if n == 0 {
				fmt.Fprintf(errOut, "  absent de la sauvegarde : %s\n", p)
				refused++
			}
		}
		slices.Sort(sel)
		sel = slices.Compact(sel)
	}
	for _, rel := range sel {
		src, dst := filepath.Join(run, filepath.FromSlash(rel)), filepath.Join(r.Home, filepath.FromSlash(rel))
		if why := restoreBlocker(r.Home, dst, r.Clones); why != "" {
			fmt.Fprintf(errOut, "  refusé (%s) : %s\n", why, Tilde(r.Home, dst))
			refused++
			continue
		}
		if r.Dry {
			fmt.Fprintf(out, "  [dry] restauré : %s\n", Tilde(r.Home, dst))
			restored++
			continue
		}
		if err := moveBack(src, dst); err != nil {
			fmt.Fprintf(errOut, "  échec : %s : %v\n", Tilde(r.Home, dst), err)
			refused++
			continue
		}
		fmt.Fprintf(out, "  restauré : %s\n", Tilde(r.Home, dst))
		restored++
		for d := filepath.Dir(src); d != run && strings.HasPrefix(d, run+string(filepath.Separator)) && os.Remove(d) == nil; d = filepath.Dir(d) {
		}
	}
	if !r.Dry && os.Remove(run) == nil { // only succeeds when empty
		fmt.Fprintf(out, "  sauvegarde vidée, dossier retiré : %s\n", Tilde(r.Home, run))
	}
	return restored, refused, nil
}

// backupRel turns a user path (~/x, x relative to ~, or absolute under ~ or under the run) into
// a clean slash path relative to ~, refusing anything that climbs out.
func backupRel(home, run, p string) (string, error) {
	bad := fmt.Errorf("chemin refusé (hors de ~ ou de la sauvegarde) : %s", p)
	if slices.Contains(strings.Split(filepath.ToSlash(p), "/"), "..") {
		return "", bad
	}
	var rel string
	switch {
	case p == "~":
		rel = "."
	case strings.HasPrefix(p, "~/"):
		rel = p[2:]
	case filepath.IsAbs(p):
		c := filepath.Clean(p)
		if r, e := filepath.Rel(run, c); e == nil && !strings.HasPrefix(r, "..") {
			rel = r
		} else if r, e := filepath.Rel(home, c); e == nil && !strings.HasPrefix(r, "..") {
			rel = r
		} else {
			return "", bad
		}
	default:
		rel = p
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", bad
	}
	return rel, nil
}

// restoreBlocker says why dst cannot be restored, or "" when it can.
func restoreBlocker(home, dst string, clones []string) string {
	rel, _ := filepath.Rel(home, dst)
	parts := strings.Split(rel, string(filepath.Separator))
	cur := home
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		i, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return err.Error()
		}
		if i.Mode()&fs.ModeSymlink != 0 {
			return "dossier parent lié"
		}
		if !i.IsDir() {
			return "parent non dossier"
		}
	}
	i, err := os.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	if err != nil {
		return err.Error()
	}
	if i.Mode()&fs.ModeSymlink == 0 {
		return "fichier existant"
	}
	t, _ := os.Readlink(dst)
	if !filepath.IsAbs(t) {
		t = filepath.Join(filepath.Dir(dst), t)
	}
	t = filepath.Clean(t)
	for _, c := range clones {
		c, _ = filepath.Abs(c)
		if inProfile(t, c) {
			return ""
		}
	}
	return "lien étranger aux profils"
}

// moveBack removes a dot link at dst, if any, and renames src there (the mode travels with it).
func moveBack(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if i, err := os.Lstat(dst); err == nil && i.Mode()&fs.ModeSymlink != 0 {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	return os.Rename(src, dst)
}
