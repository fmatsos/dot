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
		t.Fatalf("git init: %v\n%s", err, out)
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
			t.Errorf("%s: %q, %v (want %q)", label, got, err, path)
		}
	}
	want("registry default", filepath.Join(home, ".dot", "perso", "forbidden.local"))
	t.Setenv("DOT_PROFILE", "acmecorp")
	want("DOT_PROFILE", filepath.Join(home, ".dot", "acmecorp", "forbidden.local"))
	env.Profile = "perso"
	want("-p wins over DOT_PROFILE", filepath.Join(home, ".dot", "perso", "forbidden.local"))
	setProfile("globex-inc")
	want("dotfiles.profile wins over -p", filepath.Join(home, ".dot", "globex-inc", "forbidden.local"))
	t.Setenv("DOTFILES_DEPLOY", "~/elsewhere")
	want("DOTFILES_DEPLOY wins over dotfiles.profile", filepath.Join(home, "elsewhere", "forbidden.local"))
	t.Setenv("DOTFILES_FORBIDDEN", "/tmp/list")
	want("DOTFILES_FORBIDDEN wins over everything", "/tmp/list")
}

func TestTermsPathInvalidProfileKeyAndMissingRegistry(t *testing.T) {
	env, repo := guardEnv(t)
	if _, err := termsPath(env); err == nil {
		t.Fatal("missing registry accepted")
	}
	// With a registry the default profile resolves: only the invalid key can still make it fail.
	writeRegistry(t, env, twoProfiles)
	if _, err := termsPath(env); err != nil {
		t.Fatalf("control: registry present, no key: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "dotfiles.profile", "../escape").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := termsPath(env); err == nil {
		t.Fatal("invalid key accepted")
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
	// Absence of the pattern, not wording: whatever the refusal says, it must not echo the term.
	if code := execute(env, []string{"guard", "msg", "/dev/null"}); code != 1 ||
		strings.Contains(errb.String(), "secretpattern") {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
	errb.Reset()
	t.Setenv("DOTFILES_FORBIDDEN", filepath.Join(dir, "absent"))
	if code := execute(env, []string{"guard", "msg", "/dev/null"}); code != 1 {
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
	if code := execute(env, []string{"guard", "msg", msg}); code != 1 {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
	// Control: the same list lets a clean message through, so the refusal above is about the match.
	clean := filepath.Join(dir, "clean")
	if err := os.WriteFile(clean, []byte("fix a bug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := execute(env, []string{"guard", "msg", clean}); code != 0 {
		t.Fatalf("clean message: code %d, stderr %q", code, errb)
	}
}

func TestGuardUsageAndHelpDocumentsResolutionOrder(t *testing.T) {
	env, out, errb := testEnv(t)
	if code := execute(env, []string{"guard", "msg"}); code != 2 {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
	// What the help says is a message; only the exit code and the presence of a text are checked.
	if code := execute(env, []string{"guard", "--help"}); code != 0 || out.Len() == 0 {
		t.Fatalf("code %d", code)
	}
}

func TestGuardModeFromEnvAndGitConfig(t *testing.T) {
	env, repo := guardEnv(t)
	t.Setenv("DOTFILES_FORBIDDEN", filepath.Join(t.TempDir(), "absent")) // the terms list must not be needed in secrets mode
	setMode := func(v string) {
		t.Helper()
		if out, err := exec.Command("git", "-C", repo, "config", "dotfiles.guard", v).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	// Unset: normal mode, so the missing list refuses.
	if _, err := newGuard(env); err == nil {
		t.Fatal("normal mode: missing list accepted")
	}
	setMode("secrets")
	g, err := newGuard(env)
	if err != nil || g.Terms != nil {
		t.Fatalf("dotfiles.guard secrets: %v, terms %v", err, g)
	}
	// The environment wins over git config, in both directions.
	t.Setenv("DOTFILES_GUARD", "secrets")
	setMode("not-a-mode")
	if _, err := newGuard(env); err != nil {
		t.Fatalf("the environment must win: %v", err)
	}
	t.Setenv("DOTFILES_GUARD", "")
	setMode("")
	if _, err := newGuard(env); err == nil {
		t.Fatal("empty value = normal mode: missing list accepted")
	}
	// An empty value is the normal mode, not an unknown one: with a list it builds a guard with terms.
	terms := filepath.Join(t.TempDir(), "terms")
	if err := os.WriteFile(terms, []byte("acmecorp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTFILES_FORBIDDEN", terms)
	if g, err := newGuard(env); err != nil || g.Terms == nil {
		t.Fatalf("empty value with a list: %v, %v", err, g)
	}
}

func TestGuardModeUnknownValueFailsClosedWithoutEcho(t *testing.T) {
	env, repo := guardEnv(t)
	// A valid list: the unknown mode is then the only thing that can refuse.
	terms := filepath.Join(t.TempDir(), "terms")
	if err := os.WriteFile(terms, []byte("acmecorp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTFILES_FORBIDDEN", terms)
	if _, err := newGuard(env); err != nil {
		t.Fatalf("control: mode unset, valid list: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "dotfiles.guard", "zzz-secret-ish").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// Absence of the value, not wording: a refusal must never echo the mode it rejects.
	_, err := newGuard(env)
	if err == nil || strings.Contains(err.Error(), "zzz-secret-ish") {
		t.Fatalf("git config: %v", err)
	}
	t.Setenv("DOTFILES_GUARD", "zzz-env-ish") // formerly accepted as the normal mode
	_, err = newGuard(env)
	if err == nil || strings.Contains(err.Error(), "zzz-env-ish") {
		t.Fatalf("environment: %v", err)
	}
	errb := new(strings.Builder)
	env.Stderr = errb
	if code := execute(env, []string{"guard", "msg", "/dev/null"}); code != 1 ||
		strings.Contains(errb.String(), "zzz-env-ish") {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
}

func TestGuardModeGitConfigFailureFailsClosed(t *testing.T) {
	env, _ := guardEnv(t)
	// A valid list: the unreadable config is then the only thing that can refuse.
	terms := filepath.Join(t.TempDir(), "terms")
	if err := os.WriteFile(terms, []byte("acmecorp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTFILES_FORBIDDEN", terms)
	// A syntax error in the config makes `git config --get` exit 128, not 1: not "key unset".
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newGuard(env); err == nil {
		t.Fatal("unreadable config accepted")
	}
}
