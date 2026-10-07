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
	"syscall"
)

// AdoptSource validates a file to bring under a profile and returns its absolute, cleaned path and
// its path relative to home. It must sit in home (never in ~/.dot), be a regular file, not a
// symlink, and not be reached through a symlinked directory.
func AdoptSource(home, cwd, arg string) (abs, rel string, err error) {
	abs = arg
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)
	home = filepath.Clean(home)
	rel, err = filepath.Rel(home, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("hors de ~")
	}
	if rel == ".dot" || strings.HasPrefix(rel, ".dot"+string(filepath.Separator)) {
		return "", "", errors.New("dans ~/.dot")
	}
	fi, err := os.Lstat(abs)
	switch {
	case err != nil:
		return "", "", errors.New("introuvable")
	case fi.Mode()&fs.ModeSymlink != 0:
		return "", "", errors.New("lien symbolique (déjà lié ?)")
	case fi.IsDir():
		return "", "", errors.New("dossier (donne les fichiers)")
	case !fi.Mode().IsRegular():
		return "", "", errors.New("pas un fichier ordinaire")
	}
	// A parent that is a symlink may lead out of home or into a profile clone.
	realHome, err1 := filepath.EvalSymlinks(home)
	realDir, err2 := filepath.EvalSymlinks(filepath.Dir(abs))
	if err1 != nil || err2 != nil || realDir != filepath.Join(realHome, filepath.Dir(rel)) {
		return "", "", errors.New("passe par un dossier lié")
	}
	return abs, rel, nil
}

// ClaimedBy lists the keys of the profiles that already provide ~/<rel> on this machine, from
// home/ or from a variant active here.
func ClaimedBy(rel string, others []Profile) []string {
	var keys []string
	for _, p := range others {
		if ProvidedBy(p.Dir, rel) {
			keys = append(keys, p.Key)
		}
	}
	return keys
}

// AdoptMove moves src into the clone at dst and, with linkBack, puts an absolute symlink dst at
// src's place, the form Apply produces. A failed symlink moves the file back and removes the
// directories it made.
func AdoptMove(src, dst string, linkBack bool) error {
	var made []string
	for d := filepath.Dir(dst); ; d = filepath.Dir(d) {
		if _, err := os.Lstat(d); err == nil {
			break
		}
		made = append(made, d) // deepest first
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	undo := func() {
		for _, d := range made {
			_ = os.Remove(d)
		}
	}
	if err := moveFile(src, dst); err != nil {
		undo()
		return err
	}
	if !linkBack {
		return nil
	}
	if err := os.Symlink(dst, src); err != nil {
		if mv := moveFile(dst, src); mv != nil {
			return fmt.Errorf("lien impossible (%v) et fichier non restitué, il reste dans %s", err, dst)
		}
		undo()
		return err
	}
	return nil
}

// moveFile renames, falling back to copy + fsync + remove across devices; the mode is kept.
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".adopt-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	_, err = io.Copy(tmp, in)
	if err == nil {
		err = tmp.Chmod(fi.Mode().Perm())
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// IsModuleSource tells whether ~/rel is merged across profiles by dot settings or dot mcp
// rather than linked once several profiles are registered.
func IsModuleSource(rel string) bool { return slices.Contains(ModuleSources, filepath.ToSlash(rel)) }

// SafeDest refuses a destination in the clone root whose existing parents below root include a
// symlink: the file would be moved out of the clone, where dot push never sees it.
func SafeDest(root, dst string) error {
	rel, err := filepath.Rel(root, filepath.Dir(dst))
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("destination hors du clone")
	}
	d := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		d = filepath.Join(d, part)
		fi, err := os.Lstat(d)
		if err != nil {
			return nil // the rest is created by AdoptMove
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s est un lien symbolique dans le clone : le fichier en sortirait", d)
		}
	}
	return nil
}
