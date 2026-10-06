package doctor

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const manifestJSON = `{"repo":"https://git.example.com/acme/dots.git","deploy":{"sparse":["home"]},` +
	`"profiles":{"demo":"home/.config/git/profiles/demo.gitconfig"},"deployProfile":"demo",` +
	`"marketplace":{"name":"acme","plugins":["acme"]},"modules":["settings"]}`

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v : %v\n%s", args, err, out)
	}
}

// sandbox isolates HOME, git and PATH, with a fictional mise and no claude or codex.
func sandbox(t *testing.T) (home string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	bin := filepath.Join(root, "bin")
	write(t, filepath.Join(bin, "mise"), "#!/bin/sh\nexit 0\n", 0o755)
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MISE_DATA_DIR", filepath.Join(root, "mise"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("DOTFILES_FORBIDDEN", "")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Join(root, "mise", "shims")+string(os.PathListSeparator)+"/usr/bin:/bin")
	write(t, filepath.Join(home, ".config/git/profiles/demo.gitconfig"), "[user]\n  email = tester@example.com\n", 0o644)
	write(t, filepath.Join(home, ".config/git/profiles.gitconfig"), "# profiles\n", 0o644)
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte("[include]\n\tpath = ~/.config/git/profiles.gitconfig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// profile creates a healthy clone with one linked home file; it declares no mise config.
func profile(t *testing.T, home, key string) Profile { return newProfile(t, home, key, false) }

// miseProfile is profile plus a linked home/.config/mise/config.toml, which makes mise expected.
func miseProfile(t *testing.T, home, key string) Profile { return newProfile(t, home, key, true) }

func newProfile(t *testing.T, home, key string, mise bool) Profile {
	t.Helper()
	dir := filepath.Join(home, ".dot", key)
	write(t, filepath.Join(dir, "dot.json"), manifestJSON, 0o644)
	write(t, filepath.Join(dir, "home", "file-"+key), "linked\n", 0o644)
	if mise {
		write(t, filepath.Join(dir, "home", ".config", "mise", "config.toml"), "[tools]\n", 0o644)
	}
	write(t, filepath.Join(dir, "forbidden.local"), "acmecorp\n", 0o600)
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "core.hooksPath", ".githooks")
	run(t, dir, "config", "user.email", "tester@example.com")
	run(t, dir, "config", "user.name", "Tester")
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "fixture")
	if err := os.Symlink(filepath.Join(dir, "home", "file-"+key), filepath.Join(home, "file-"+key)); err != nil {
		t.Fatal(err)
	}
	if mise {
		link := filepath.Join(home, ".config", "mise", "config.toml")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "home", ".config", "mise", "config.toml"), link); err != nil {
			t.Fatal(err)
		}
	}
	return Profile{Key: key, Dir: dir}
}

func check(home string, ps ...Profile) []Line {
	return Run(Options{Home: home, Profiles: ps, Headers: len(ps) > 1,
		SrcRoot: filepath.Join(home, "src"), SandboxRoot: filepath.Join(home, "sandbox")})
}

func has(lines []Line, l Level, msg string) bool {
	return slices.Contains(lines, Line{l, msg})
}

func hasPrefix(lines []Line, l Level, prefix string) bool {
	return slices.ContainsFunc(lines, func(x Line) bool { return x.Level == l && strings.HasPrefix(x.Message, prefix) })
}

func text(lines []Line) string {
	var b bytes.Buffer
	_ = Write(&b, lines, false)
	return b.String()
}

func TestHealthySingleProfileKeepsHistoricalOrder(t *testing.T) {
	home := sandbox(t)
	lines := check(home, miseProfile(t, home, "alpha"))
	if HasFail(lines) {
		t.Fatalf("échec inattendu :\n%s", text(lines))
	}
	want := []string{
		"clone déployé présent", "hooks du clone déployé", "email du clone : profil demo", "clone sans changements",
		"clone sans avance ni retard connus", "profil demo", "liens home à jour", "aucun lien mort dans ~/.local/bin",
		"garde-fou activé", "profils inclus dans le git config global", "mise présent", "outils mise installés",
		"shims mise dans PATH", "aucun secret déclaré", "identités des dépôts cohérentes",
	}
	var got []string
	for _, l := range lines {
		if l.Level == Raw {
			t.Errorf("ligne brute inattendue : %q", l.Message)
		}
		got = append(got, l.Message)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("ordre ou libellés différents :\n%s", text(lines))
	}
}

func TestFailuresAndWarnings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, home string, p Profile)
		level Level
		msg   string
	}{
		{"fichier détaché", func(t *testing.T, home string, p Profile) {
			os.Remove(filepath.Join(home, "file-alpha"))
			write(t, filepath.Join(home, "file-alpha"), "x", 0o644)
		}, Fail, "1 fichiers détachés : relancer install.sh après réconciliation"},
		{"liste vide", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "forbidden.local"), "  # note\n\n \t\n", 0o600)
		}, Fail, "garde-fou désactivé : restaurer forbidden.local"},
		{"liste invalide", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "forbidden.local"), "acmecorp\n[\n", 0o600)
		}, Fail, "garde-fou désactivé : liste de termes invalide, corriger forbidden.local"},
		{"liste à opérateur GNU", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "forbidden.local"), "acmecorp\\|globex\n", 0o600)
		}, Fail, "garde-fou désactivé : liste de termes invalide, corriger forbidden.local"},
		{"email du clone", func(t *testing.T, _ string, p Profile) {
			run(t, p.Dir, "config", "user.email", "other@example.com")
		}, Fail, "email du clone : rétablir le profil demo (install.sh)"},
		{"hooks", func(t *testing.T, _ string, p Profile) {
			run(t, p.Dir, "config", "core.hooksPath", ".husky")
		}, Fail, "hooks du clone : relancer install.sh"},
		{"clone modifié", func(t *testing.T, _ string, p Profile) {
			write(t, filepath.Join(p.Dir, "new"), "x", 0o644)
		}, Warn, "clone modifié : dot push"},
		{"lien mort", func(t *testing.T, home string, _ Profile) {
			write(t, filepath.Join(home, ".local/bin/.keep"), "", 0o644)
			os.Symlink(filepath.Join(home, "absent"), filepath.Join(home, ".local/bin/dead"))
		}, Fail, "1 liens morts dans ~/.local/bin : relancer install.sh"},
		{"profil git sans email", func(t *testing.T, home string, _ Profile) {
			write(t, filepath.Join(home, ".config/git/profiles/demo.gitconfig"), "[user]\n  name = T\n", 0o644)
		}, Fail, "profil demo : fichier absent ou user.email manquant"},
		{"shims hors PATH", func(t *testing.T, _ string, _ Profile) { t.Setenv("MISE_DATA_DIR", "/nonexistent") },
			Warn, "shims mise hors PATH : charger ~/.config/mise/shell.sh"},
		{"include absent", func(t *testing.T, _ string, _ Profile) {
			write(t, os.Getenv("GIT_CONFIG_GLOBAL"), "", 0o644)
		}, Fail, "profils non inclus : relancer install.sh"},
		{"secrets verrouillés", func(t *testing.T, _ string, p Profile) {
			t.Setenv("BW_SESSION", "")
			write(t, filepath.Join(p.Dir, "secrets.local"), "FAKE_TOKEN=bw:fictional-item\n", 0o600)
		}, Warn, "secrets : 0 lisibles / 1 verrouillés / 0 indisponibles (statut incomplet ou accès bloqué) : dot secrets unlock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := sandbox(t)
			p := profile(t, home, "alpha")
			tc.setup(t, home, p)
			lines := check(home, p)
			if !has(lines, tc.level, tc.msg) {
				t.Fatalf("manque %s %q :\n%s", tc.level.Symbol(), tc.msg, text(lines))
			}
			if out := text(lines); strings.Contains(out, "fictional-item") || strings.Contains(out, "acmecorp") {
				t.Errorf("valeur sensible affichée :\n%s", out)
			}
		})
	}
}

func TestGuardUsesTheGuardValidationAndNeverShowsPatterns(t *testing.T) {
	home := sandbox(t)
	p := profile(t, home, "alpha")
	write(t, filepath.Join(p.Dir, "forbidden.local"), "globex-secret(\n", 0o600)
	lines := check(home, p)
	if !has(lines, Fail, "garde-fou désactivé : liste de termes invalide, corriger forbidden.local") || has(lines, OK, "garde-fou activé") {
		t.Fatalf("liste invalide acceptée :\n%s", text(lines))
	}
	if strings.Contains(text(lines), "globex") {
		t.Errorf("motif affiché :\n%s", text(lines))
	}
	// $DOTFILES_FORBIDDEN is validated the same way.
	list := filepath.Join(home, "list")
	write(t, list, "acmecorp\n", 0o600)
	t.Setenv("DOTFILES_FORBIDDEN", list)
	if lines := check(home, p); !has(lines, OK, "garde-fou activé") {
		t.Errorf("liste valide via DOTFILES_FORBIDDEN :\n%s", text(lines))
	}
	write(t, list, "[\n", 0o600)
	if lines := check(home, p); !hasPrefix(lines, Fail, "garde-fou désactivé : liste de termes invalide") {
		t.Errorf("liste invalide via DOTFILES_FORBIDDEN :\n%s", text(lines))
	}
}

func TestDetachedListIsCappedAtTen(t *testing.T) {
	home := sandbox(t)
	p := profile(t, home, "alpha")
	for i := 0; i < 12; i++ {
		write(t, filepath.Join(p.Dir, "home", "extra", string(rune('a'+i))), "x", 0o644)
	}
	lines := check(home, p)
	if !hasPrefix(lines, Fail, "12 fichiers détachés") {
		t.Fatalf("compteur absent :\n%s", text(lines))
	}
	raw := 0
	for _, l := range lines {
		if l.Level == Raw && strings.HasPrefix(l.Message, "  ~/extra/") {
			raw++
		}
	}
	if raw != 10 {
		t.Errorf("%d fichiers listés, attendu 10", raw)
	}
}

func TestMissingCloneAndMiseTools(t *testing.T) {
	home := sandbox(t)
	lines := check(home, Profile{Key: "gone", Dir: filepath.Join(home, ".dot", "gone")})
	for _, l := range []Line{
		{Fail, "clone déployé absent : relancer install.sh"}, {Fail, "dot.json invalide : clé objet"},
		{Warn, "profils et marketplaces ignorés : dot.json invalide"}, {Fail, "home du clone absent : relancer install.sh"},
		{Fail, "garde-fou désactivé : restaurer forbidden.local"}, {OK, "aucun secret déclaré"},
	} {
		if !has(lines, l.Level, l.Message) {
			t.Errorf("manque %s %q", l.Level.Symbol(), l.Message)
		}
	}
	write(t, filepath.Join(filepath.Dir(os.Getenv("MISE_DATA_DIR")), "bin", "mise"),
		"#!/bin/sh\n[ \"$MISE_OFFLINE\" = true ] || exit 1\nprintf 'node 1\\nnode 2\\npython 3\\n'\n", 0o755)
	if lines := check(home); !has(lines, Fail, "outils mise manquants : node,python — mise install") {
		t.Errorf("outils manquants :\n%s", text(lines))
	}
	write(t, filepath.Join(filepath.Dir(os.Getenv("MISE_DATA_DIR")), "bin", "mise"), "#!/bin/sh\nexit 1\n", 0o755)
	if lines := check(home); !has(lines, Fail, "outils mise invérifiables : vérifier mise ls --missing") {
		t.Errorf("mise en échec :\n%s", text(lines))
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	os.Remove(filepath.Join(filepath.Dir(os.Getenv("MISE_DATA_DIR")), "bin", "mise"))
	if lines := check(home); hasMise(lines) {
		t.Errorf("mise sans profil ni binaire, rien attendu :\n%s", text(lines))
	}
	if lines := check(home, miseProfile(t, home, "alpha")); !has(lines, Fail, "mise absent : relancer install.sh") {
		t.Errorf("mise absent :\n%s", text(lines))
	}
}

func hasMise(lines []Line) bool {
	return slices.ContainsFunc(lines, func(l Line) bool { return strings.Contains(l.Message, "mise") })
}

// noMise removes the sandbox mise from the PATH, as on a machine whose profiles do not ask for it.
func noMise(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", "/usr/bin:/bin")
}

func TestMiseChecksFollowTheProfileConfig(t *testing.T) {
	t.Run("sans config ni mise : silence, code 0", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		lines := check(home, profile(t, home, "alpha"))
		if hasMise(lines) {
			t.Errorf("ligne mise inattendue :\n%s", text(lines))
		}
		if HasFail(lines) {
			t.Errorf("échec inattendu :\n%s", text(lines))
		}
	})
	t.Run("config sans mise : échec", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		lines := check(home, miseProfile(t, home, "alpha"))
		if !has(lines, Fail, "mise absent : relancer install.sh") {
			t.Errorf("mise absent attendu :\n%s", text(lines))
		}
		if !hasPrefix(lines, Warn, "shims mise hors PATH") { // the config asks for mise: shims are checked too
			t.Errorf("shims non vérifiés :\n%s", text(lines))
		}
	})
	t.Run("sans config, mise dans ~/.local/bin : vérifié", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		write(t, filepath.Join(home, ".local", "bin", "mise"), "#!/bin/sh\nexit 0\n", 0o755)
		lines := check(home, profile(t, home, "alpha"))
		if !has(lines, OK, "mise présent") || !has(lines, OK, "outils mise installés") {
			t.Errorf("mise présent attendu :\n%s", text(lines))
		}
	})
	t.Run("sans config, mise dans PATH : vérifié", func(t *testing.T) {
		home := sandbox(t)
		if lines := check(home, profile(t, home, "alpha")); !has(lines, OK, "mise présent") {
			t.Errorf("mise présent attendu :\n%s", text(lines))
		}
	})
	t.Run("un seul profil sur deux déclare la config", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		lines := check(home, profile(t, home, "alpha"), miseProfile(t, home, "beta"))
		if !has(lines, Fail, "mise absent : relancer install.sh") {
			t.Errorf("mise absent attendu :\n%s", text(lines))
		}
	})
	t.Run("ni profil ni mise : silence", func(t *testing.T) {
		home := sandbox(t)
		noMise(t)
		if lines := check(home); hasMise(lines) {
			t.Errorf("ligne mise inattendue :\n%s", text(lines))
		}
	})
}

func TestMultiProfileHeadersAndMachineChecksOnce(t *testing.T) {
	home := sandbox(t)
	a, b := profile(t, home, "alpha"), profile(t, home, "beta")
	write(t, filepath.Join(b.Dir, "forbidden.local"), "", 0o600)
	lines := check(home, a, b)
	var order []string
	for _, l := range lines {
		if strings.HasPrefix(l.Message, "profil ") && l.Level == Raw {
			order = append(order, l.Message)
		}
	}
	if !slices.Equal(order, []string{"profil alpha", "profil beta"}) {
		t.Fatalf("en-têtes : %v", order)
	}
	count := func(msg string) (n int) {
		for _, l := range lines {
			if l.Message == msg {
				n++
			}
		}
		return n
	}
	for _, once := range []string{"aucun lien mort dans ~/.local/bin", "mise présent", "shims mise dans PATH",
		"profils inclus dans le git config global", "identités des dépôts cohérentes"} {
		if n := count(once); n != 1 {
			t.Errorf("%q : %d fois", once, n)
		}
	}
	if count("clone déployé présent") != 2 || count("garde-fou activé") != 1 || count("garde-fou désactivé : restaurer forbidden.local") != 1 {
		t.Errorf("vérifications par profil :\n%s", text(lines))
	}
	idx := func(msg string) int { return slices.IndexFunc(lines, func(l Line) bool { return l.Message == msg }) }
	if idx("aucun lien mort dans ~/.local/bin") < idx("profil beta") || idx("garde-fou désactivé : restaurer forbidden.local") < idx("profil beta") {
		t.Errorf("les profils précèdent les vérifications de la machine :\n%s", text(lines))
	}
}

func TestTargetErrorAndInvalidManifest(t *testing.T) {
	home := sandbox(t)
	lines := Run(Options{Home: home, Err: errors.New("aucun profil inscrit (dot install <url>)"),
		SrcRoot: filepath.Join(home, "src"), SandboxRoot: filepath.Join(home, "sandbox")})
	if lines[0] != (Line{Fail, "aucun profil inscrit (dot install <url>)"}) || !has(lines, OK, "mise présent") {
		t.Errorf("registre vide :\n%s", text(lines))
	}
	p := profile(t, home, "alpha")
	write(t, filepath.Join(p.Dir, "dot.json"), `{"unexpected":true}`, 0o644)
	lines = check(home, p)
	if !has(lines, Fail, "dot.json invalide : clé unexpected") || !has(lines, Warn, "profils et marketplaces ignorés : dot.json invalide") {
		t.Errorf("manifeste invalide :\n%s", text(lines))
	}
	for _, l := range lines {
		if strings.HasPrefix(l.Message, "email du clone") || strings.HasPrefix(l.Message, "profil demo") {
			t.Errorf("vérification dépendante du manifeste exécutée : %q", l.Message)
		}
	}
}

func TestWrite(t *testing.T) {
	lines := []Line{{OK, "a"}, {Warn, "b"}, {Fail, "c"}, {Raw, "  d"}}
	if got := text(lines); got != "✓ a\n! b\n✗ c\n  d\n" {
		t.Errorf("sans couleur : %q", got)
	}
	var b bytes.Buffer
	_ = Write(&b, lines, true)
	if want := "\033[32m✓\033[0m a\n\033[33m!\033[0m b\n\033[31m✗\033[0m c\n  d\n"; b.String() != want {
		t.Errorf("avec couleur : %q", b.String())
	}
	if HasFail(lines[:2]) || !HasFail(lines) {
		t.Error("HasFail")
	}
}
