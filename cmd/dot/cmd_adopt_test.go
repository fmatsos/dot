package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// adoptEnv sets up two profiles (perso is the default, with a term list) and a scanner that
// accepts everything except content holding "SECRETTOKEN".
func adoptEnv(t *testing.T) *Env {
	t.Helper()
	env, _, _ := testEnv(t)
	t.Setenv("DOTFILES_FORBIDDEN", "")
	t.Setenv("DOTFILES_GUARD", "")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	writeRegistry(t, env, twoProfiles)
	scan := filepath.Join(t.TempDir(), "scan")
	script := "#!/bin/sh\n[ \"$3\" = dir ] || exit 2\ngrep -q SECRETTOKEN \"$4\" && exit 1\nexit 0\n"
	if err := os.WriteFile(scan, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOT_BETTERLEAKS", scan)
	for _, k := range []string{"perso", "acmecorp"} {
		if err := os.MkdirAll(filepath.Join(env.ProfileDirFor(k), "home"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(env.ProfileDirFor("perso"), "forbidden.local"), "acmecorp\n")
	write(t, filepath.Join(env.ProfileDirFor("acmecorp"), "forbidden.local"), "globex\n")
	return env
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestAdoptHappyPathAndDryRun(t *testing.T) {
	env := adoptEnv(t)
	f := filepath.Join(env.Home, ".config", "app", "rc")
	write(t, f, "hello\n")
	if code := execute(env, []string{"adopt", "-n", f}); code != 0 {
		t.Fatalf("-n : code %d", code)
	}
	if fi, err := os.Lstat(f); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("-n a touché le fichier : %v", err)
	}
	if code := execute(env, []string{"adopt", f}); code != 0 {
		t.Fatalf("code %d", code)
	}
	dst := filepath.Join(env.ProfileDirFor("perso"), "home", ".config", "app", "rc")
	if got, _ := os.Readlink(f); got != dst {
		t.Fatalf("lien = %q, attendu %q", got, dst)
	}
	if b, _ := os.ReadFile(dst); string(b) != "hello\n" {
		t.Fatalf("contenu = %q", b)
	}
	if fi, _ := os.Stat(dst); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", fi.Mode())
	}
	// Once linked, the file is a symlink: refused.
	if code := execute(env, []string{"adopt", f}); code != 1 {
		t.Fatalf("relance : code %d", code)
	}
}

func TestAdoptRefusals(t *testing.T) {
	env := adoptEnv(t)
	errb := env.Stderr.(interface{ String() string })
	dir := env.ProfileDirFor("perso")
	outside := filepath.Join(t.TempDir(), "x")
	write(t, outside, "x\n")
	term := filepath.Join(env.Home, ".term")
	write(t, term, "tenant AcmeCorp\n")
	sec := filepath.Join(env.Home, ".sec")
	write(t, sec, "SECRETTOKEN\n")
	exist := filepath.Join(env.Home, ".exist")
	write(t, exist, "a\n")
	write(t, filepath.Join(dir, "home", ".exist"), "b\n")
	other := filepath.Join(env.Home, ".other")
	write(t, other, "a\n")
	write(t, filepath.Join(env.ProfileDirFor("acmecorp"), "home", ".other"), "b\n")
	dirArg := filepath.Join(env.Home, "adir")
	write(t, filepath.Join(dirArg, "f"), "f\n")
	inDot := filepath.Join(env.Home, ".dot", "note")
	write(t, inDot, "n\n")
	sym := filepath.Join(env.Home, ".sym")
	if err := os.Symlink(exist, sym); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"hors de ~": outside, "terme": term, "secret": sec, "existe": exist,
		"autre profil": other, "dossier": dirArg, "~/.dot": inDot, "lien": sym, "..": filepath.Join(env.Home, "..", filepath.Base(outside))} {
		before, _ := os.Lstat(p)
		if code := execute(env, []string{"adopt", p}); code != 1 {
			t.Errorf("%s : code %d", name, code)
		}
		if after, err := os.Lstat(p); before != nil && (err != nil || after.Mode() != before.Mode()) {
			t.Errorf("%s : fichier modifié", name)
		}
	}
	if strings.Contains(errb.String(), "AcmeCorp") || strings.Contains(errb.String(), "SECRETTOKEN") {
		t.Errorf("texte trouvé rapporté : %s", errb)
	}
	if _, err := os.Lstat(filepath.Join(dir, "home", ".term")); err == nil {
		t.Error("fichier à terme interdit déplacé")
	}
}

func TestAdoptSparseWithoutHome(t *testing.T) {
	env := adoptEnv(t)
	write(t, filepath.Join(env.ProfileDirFor("perso"), "dot.json"),
		`{"repo":"r","deploy":{"sparse":["bin"]},"profiles":{"a":"home/a.gitconfig"},"deployProfile":"a","modules":[]}`)
	f := filepath.Join(env.Home, ".f")
	write(t, f, "x\n")
	if code := execute(env, []string{"adopt", f}); code != 1 {
		t.Fatalf("code %d", code)
	}
	if fi, err := os.Lstat(f); err != nil || !fi.Mode().IsRegular() {
		t.Fatal("fichier déplacé")
	}
}

func TestAdoptFailsClosedWithoutTerms(t *testing.T) {
	env := adoptEnv(t)
	if err := os.Remove(filepath.Join(env.ProfileDirFor("perso"), "forbidden.local")); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(env.Home, ".f")
	write(t, f, "x\n")
	if code := execute(env, []string{"adopt", f}); code != 1 {
		t.Fatalf("code %d", code)
	}
	if fi, err := os.Lstat(f); err != nil || !fi.Mode().IsRegular() {
		t.Fatal("fichier déplacé")
	}
}
