package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fmatsos/dot/internal/link"
	"github.com/fmatsos/dot/internal/manifest"
	"github.com/fmatsos/dot/internal/registry"
)

// DeriveKey builds a profile key from a repository URL or path: its last segment without .git.
func DeriveKey(url string) (string, error) {
	s := strings.TrimRight(url, "/")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ".git")
	if err := (&registry.Registry{}).Add(s, "x"); err != nil {
		return "", fmt.Errorf("clé de profil impossible à déduire de %s (utiliser -p <clé>)", url)
	}
	return s, nil
}

// Add installs a new profile: partial clone into ~/.dot/<key>, manifest and identity checks,
// conflict check against the registered profiles, then registration and the installation steps.
// Nothing is registered before the clone and the manifest are valid and no link conflicts; a
// failure up to there removes the clone this call created. Later failures keep the registered
// profile: `dot install` repairs it. A dry run without clone previews and creates nothing.
func (in *Installer) Add(url, key string) (err error) {
	if i, serr := os.Stat(url); serr == nil && i.IsDir() { // a relative local path would not survive in the clone's remote
		url, _ = filepath.Abs(url)
	}
	if err := (&registry.Registry{}).Add(key, url); err != nil {
		return err
	}
	r, err := registry.Load(in.regPath())
	if err != nil {
		return err
	}
	if i := slices.Index(r.Keys(), key); i >= 0 && r.Profiles[i].Repo != url {
		return fmt.Errorf("profil %s déjà inscrit avec un autre dépôt : %s", key, r.Profiles[i].Repo)
	}
	dir := in.dirOf(key)
	created, err := in.ensureClone(dir, url)
	committed := false
	defer func() {
		if err != nil && created && !committed && !in.Dry {
			_ = os.RemoveAll(dir)
		}
	}()
	if err != nil {
		return err
	}
	if in.Dry && created { // nothing cloned: no manifest to read
		fmt.Fprintln(in.Out, &link.NoHomeError{Dir: dir})
		return nil
	}
	if created {
		m, err := manifest.Load(filepath.Join(dir, "dot.json"))
		if err != nil {
			return err
		}
		if err := in.run(dir, append([]string{"sparse-checkout", "set", "--cone"}, m.Sparse...)...); err != nil {
			return err
		}
	}
	p := Profile{key, dir}
	all, err := in.all([]Profile{p})
	if err != nil {
		return err
	}
	commit := func() error {
		if !in.Dry && !slices.Contains(r.Keys(), key) {
			if err := r.Add(key, url); err != nil {
				return err
			}
			if err := r.Save(in.regPath()); err != nil {
				return err
			}
		}
		committed = true
		return nil
	}
	_, err = in.install([]Profile{p}, all, false, commit)
	return err
}

// ensureClone makes dir a blobless sparse clone of url, bootstrapped with the root files only
// (dot.json comes with them; the manifest then names the real cone). created tells whether this
// call had to create it, even when it failed halfway.
func (in *Installer) ensureClone(dir, url string) (created bool, err error) {
	if _, serr := os.Stat(filepath.Join(dir, ".git")); serr == nil {
		if out, _ := gitOut(dir, "config", "remote.origin.url"); strings.TrimSpace(string(out)) != url {
			return false, fmt.Errorf("%s est déjà un clone d'un autre dépôt", dir)
		}
		return false, nil
	}
	if entries, rerr := os.ReadDir(dir); rerr == nil && len(entries) > 0 {
		return false, fmt.Errorf("%s existe déjà et n'est pas un clone git", dir)
	}
	branch, berr := defaultBranch(url)
	if berr != nil {
		if !in.Dry {
			return false, fmt.Errorf("clone de %s impossible : %w", url, berr)
		}
		branch = "main" // ponytail: a dry run previews with main when the remote cannot be asked
	}
	fmt.Fprintf(in.Out, "clone partiel → %s\n", dir)
	if in.Dry {
		fmt.Fprintf(in.Out, "  [dry] mkdir -p %s\n", dir)
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	for _, a := range [][]string{
		{"init", "-q", "-b", branch},
		{"remote", "add", "origin", url},
		{"config", "remote.origin.promisor", "true"},
		{"config", "remote.origin.partialclonefilter", "blob:none"},
		{"sparse-checkout", "set", "--cone"},
		{"fetch", "-q", "--filter=blob:none", "origin", branch},
		{"checkout", "-q", branch},
	} {
		if err := in.run(dir, a...); err != nil {
			return true, fmt.Errorf("clone de %s impossible : %w", url, err)
		}
	}
	return true, nil
}

// defaultBranch asks the remote for the branch its HEAD points to. Without a symref (detached
// HEAD) it keeps main when refs/heads/main exists; an empty or unreachable remote is an error.
func defaultBranch(url string) (string, error) {
	out, err := gitOut("", "ls-remote", "--symref", url, "HEAD", "refs/heads/main")
	if err != nil {
		return "", err
	}
	hasMain := false
	for _, line := range strings.Split(string(out), "\n") {
		if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
			if b, _, _ := strings.Cut(ref, "\t"); b != "" && !strings.HasPrefix(b, "-") {
				return b, nil
			}
		}
		if strings.HasSuffix(line, "\trefs/heads/main") {
			hasMain = true
		}
	}
	if hasMain {
		return "main", nil
	}
	return "", errors.New("branche par défaut introuvable (dépôt vide ?)")
}

// Uninstall removes the links of a profile and its registry entry. The clone stays unless purge;
// purging refuses anything but ~/.dot/<key> itself (no link) and a clone with uncommitted changes
// (unless force). Every refusal comes before the first change.
func (in *Installer) Uninstall(key string, purge, force bool) error {
	r, err := registry.Load(in.regPath())
	if err != nil {
		return err
	}
	if !slices.Contains(r.Keys(), key) {
		return fmt.Errorf("profil inconnu : %s (profils inscrits : %s)", key, strings.Join(r.Keys(), ", "))
	}
	dir := in.dirOf(key)
	if purge {
		if err := in.checkPurge(dir, force); err != nil {
			return err
		}
	}
	if err := r.Remove(key); err != nil {
		return err
	}
	if err := link.Unlink(dir, in.Home, in.Out); err != nil {
		return err
	}
	if err := r.Save(in.regPath()); err != nil {
		return err
	}
	if rest := r.Keys(); len(rest) == 1 {
		in.relinkHint(in.dirOf(rest[0]))
	}
	if !purge {
		fmt.Fprintf(in.Out, "profil %s désinstallé (clone gardé : %s)\n", key, link.Tilde(in.Home, dir))
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	fmt.Fprintf(in.Out, "profil %s désinstallé (clone supprimé : %s)\n", key, link.Tilde(in.Home, dir))
	return nil
}

// relinkHint tells that the last profile left does not get its ModuleSources linked back by an uninstall.
func (in *Installer) relinkHint(dir string) {
	for _, rel := range link.ModuleSources {
		src, dst := filepath.Join(dir, "home", rel), filepath.Join(in.Home, rel)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if t, err := os.Readlink(dst); err != nil || t != src {
			fmt.Fprintf(in.Out, "  %s n'est pas relié (un seul profil reste) : `dot install` le reliera\n", link.Tilde(in.Home, dst))
		}
	}
}

func (in *Installer) checkPurge(dir string, force bool) error {
	if filepath.Dir(dir) != filepath.Join(in.Home, ".dot") {
		return fmt.Errorf("suppression refusée : %s n'est pas un clone sous ~/.dot", dir)
	}
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("suppression refusée : %s est un lien ou n'est pas un dossier", dir)
	}
	if force {
		return nil
	}
	out, err := gitOut(dir, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("suppression refusée : état de %s illisible (--force pour passer outre)", dir)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return fmt.Errorf("suppression refusée : %s a des changements non commités (--force pour passer outre)", dir)
	}
	return nil
}
