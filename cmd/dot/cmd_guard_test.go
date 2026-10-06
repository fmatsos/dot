package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// guardEnv isolates git config and the variables the resolution reads, inside a fresh repository.
func guardEnv(t *testing.T) (*Env, string) {
	t.Helper()
	env, _, _ := testEnv(t)
	for _, k := range []string{"DOTFILES_FORBIDDEN", "DOTFILES_GUARD", "DOT_BETTERLEAKS"} {
		t.Setenv(k, "")
	}
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v\n%s", err, out)
	}
	t.Chdir(repo)
	return env, repo
}

func TestTermsPathResolutionOrder(t *testing.T) {
	env, repo := guardEnv(t)
	writeRegistry(t, env, twoProfiles) // default: perso
	setProfile := func(key string) {
		t.Helper()
		if out, err := exec.Command("git", "-C", repo, "config", "dotfiles.profile", key).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	home := env.Home
	want := func(label, path string) {
		t.Helper()
		got, err := termsPath(env)
		if err != nil || got != path {
			t.Errorf("%s : %q, %v (attendu %q)", label, got, err, path)
		}
	}
	want("défaut du registre", filepath.Join(home, ".dot", "perso", "forbidden.local"))
	t.Setenv("DOT_PROFILE", "acmecorp")
	want("DOT_PROFILE", filepath.Join(home, ".dot", "acmecorp", "forbidden.local"))
	env.Profile = "perso"
	want("-p prime sur DOT_PROFILE", filepath.Join(home, ".dot", "perso", "forbidden.local"))
	setProfile("globex-inc")
	want("dotfiles.profile prime sur -p", filepath.Join(home, ".dot", "globex-inc", "forbidden.local"))
	t.Setenv("DOTFILES_DEPLOY", "~/elsewhere")
	want("DOTFILES_DEPLOY prime sur dotfiles.profile", filepath.Join(home, "elsewhere", "forbidden.local"))
	t.Setenv("DOTFILES_FORBIDDEN", "/tmp/list")
	want("DOTFILES_FORBIDDEN prime sur tout", "/tmp/list")
}

func TestTermsPathInvalidProfileKeyAndMissingRegistry(t *testing.T) {
	env, repo := guardEnv(t)
	if _, err := termsPath(env); err == nil || !strings.Contains(err.Error(), "aucun profil inscrit") {
		t.Fatalf("registre absent : %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "dotfiles.profile", "../escape").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := termsPath(env); err == nil || !strings.Contains(err.Error(), "dotfiles.profile invalide") {
		t.Fatalf("clé invalide : %v", err)
	}
}

func TestGuardMsgFailsClosedAndNeverPrintsPattern(t *testing.T) {
	env, _, errb := testEnv(t)
	dir := t.TempDir()
	terms := filepath.Join(dir, "terms")
	if err := os.WriteFile(terms, []byte("acmecorp\nsecretpattern(\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTFILES_FORBIDDEN", terms)
	t.Setenv("DOTFILES_GUARD", "")
	if code := execute(env, []string{"guard", "msg", "/dev/null"}); code != 1 ||
		!strings.HasPrefix(errb.String(), "guard: liste de termes interdits invalide ou illisible ("+terms+"), refus.") ||
		strings.Contains(errb.String(), "secretpattern") {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
	errb.Reset()
	t.Setenv("DOTFILES_FORBIDDEN", filepath.Join(dir, "absent"))
	if code := execute(env, []string{"guard", "msg", "/dev/null"}); code != 1 || !strings.Contains(errb.String(), "absente ou vide") {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
}

func TestGuardMsgBlocksAMatch(t *testing.T) {
	env, _, errb := testEnv(t)
	dir := t.TempDir()
	terms, msg := filepath.Join(dir, "terms"), filepath.Join(dir, "msg")
	for p, c := range map[string]string{terms: "acmecorp\n", msg: "fix for AcmeCorp\n"} {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DOTFILES_FORBIDDEN", terms)
	t.Setenv("DOTFILES_GUARD", "")
	if code := execute(env, []string{"guard", "msg", msg}); code != 1 || errb.String() != "guard: référence interdite dans le message de commit.\n" {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
}

func TestGuardUsageAndHelpDocumentsResolutionOrder(t *testing.T) {
	env, out, errb := testEnv(t)
	if code := execute(env, []string{"guard", "msg"}); code != 2 || errb.String() != "usage : dot guard msg <fichier>\n" {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
	if code := execute(env, []string{"guard", "--help"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"$DOTFILES_FORBIDDEN", "$DOTFILES_DEPLOY", "dotfiles.profile", "$DOT_PROFILE", "DOTFILES_GUARD=secrets", "$DOT_BETTERLEAKS", "staged", "msg", "push", "all"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("aide sans %q", want)
		}
	}
}
