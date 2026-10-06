package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRoute(t *testing.T) {
	env, _, _ := testEnv(t)
	root := newRoot(env)
	for _, tc := range []struct {
		args     []string
		rest     []string
		external bool
		profile  string
	}{
		{[]string{"log", "--oneline"}, []string{"log", "--oneline"}, true, ""},
		{[]string{"-p", "acmecorp", "log"}, []string{"log"}, true, "acmecorp"},
		{[]string{"--profile=acmecorp", "diff"}, []string{"diff"}, true, "acmecorp"},
		{[]string{"-pacmecorp", "clone", "-p", "x"}, []string{"clone", "-p", "x"}, false, "acmecorp"},
		{[]string{"st"}, []string{"st"}, false, ""},
		{[]string{"status"}, []string{"status"}, false, ""},
		{[]string{"push", "msg"}, []string{"push", "msg"}, false, ""},
		{[]string{"-p", "a", "push"}, []string{"push"}, false, "a"},
		{[]string{"send"}, []string{"send"}, true, ""},
		{[]string{"help", "nope"}, []string{"help", "nope"}, false, ""},
		{[]string{"--nope"}, []string{"--nope"}, false, ""},
		{[]string{"--help"}, []string{"--help"}, false, ""},
		{[]string{"__complete", "st"}, []string{"__complete", "st"}, false, ""},
		{[]string{"completion", "zsh"}, []string{"completion", "zsh"}, false, ""},
		{[]string{"-p"}, []string{"-p"}, false, ""},
		{nil, nil, false, ""},
	} {
		env.Profile = ""
		rest, ext := route(env, root, tc.args)
		if ext != tc.external || !slices.Equal(rest, tc.rest) || env.Profile != tc.profile {
			t.Errorf("route(%v) = %v, %v, profil %q", tc.args, rest, ext, env.Profile)
		}
	}
}

func TestCloneCmd(t *testing.T) {
	env, out, errb := testEnv(t)
	t.Setenv("DOT_SRC", filepath.Join(env.Home, "s"))
	if code := execute(env, []string{"clone", "--path", "git@git.example.com:team/sub/p.git"}); code != 0 ||
		out.String() != filepath.Join(env.Home, "s/git.example.com/team/p")+"\n" {
		t.Fatalf("code %d, %q, %q", code, out, errb)
	}
	for _, args := range [][]string{{"clone"}, {"clone", "--path"}, {"clone", "nope"}, {"clone", "--path", "https://git.example.com/a/b", "x"}} {
		errb.Reset()
		if code := execute(env, args); code != 2 || !strings.HasPrefix(errb.String(), "usage : dot clone [--path] <url>") {
			t.Errorf("%v : code %d, %q", args, code, errb)
		}
	}
	out.Reset()
	if code := execute(env, []string{"clone", "--help"}); code != 0 || !strings.Contains(out.String(), "usage : dot clone [--path]") {
		t.Errorf("clone --help : code %d, %q", code, out)
	}
}

func TestHelpUnknownTopicIsUsage(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"help", "nope"}); code != 2 || errb.String() != "dot : commande inconnue : nope\n" {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestProfileRuleIsIdempotent(t *testing.T) {
	env, out, errb := testEnv(t)
	prof := filepath.Join(env.Home, ".config/git/profiles")
	if err := os.MkdirAll(prof, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prof, "perso.local"), []byte("# fictional\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(env.Home, "project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"règle ajoutée : gitdir:" + proj + "/ → perso\n", "règle déjà présente : gitdir:" + proj + "/ → perso\n"} {
		out.Reset()
		if code := execute(env, []string{"profile", "perso", proj}); code != 0 || out.String() != want {
			t.Fatalf("passe %d : code %d, %q, %q", i, code, out, errb)
		}
	}
	got, _ := os.ReadFile(filepath.Join(env.Home, ".config/git/profiles.local"))
	want := "[includeIf \"gitdir:" + proj + "/\"]\n\tpath = ~/.config/git/profiles/perso.local\n"
	if string(got) != want {
		t.Fatalf("profiles.local = %q", got)
	}
	errb.Reset()
	if code := execute(env, []string{"profile", "a..b", proj}); code != 1 || !strings.Contains(errb.String(), "profil : nom invalide") {
		t.Fatalf("nom invalide : code %d, %q", code, errb)
	}
	if code := execute(env, []string{"profile", "perso", "git@example.com:me/**"}); code != 0 || !strings.Contains(out.String(), "hasconfig:remote.*.url:git@example.com:me/**") {
		t.Fatalf("remote : code %d, %q", code, out)
	}
}

func TestStatusUsesDriftReport(t *testing.T) {
	env, _, errb := testEnv(t)
	dir := t.TempDir()
	t.Setenv("DOTFILES_DEPLOY", dir)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "gitconfig"))
	if err := os.WriteFile(filepath.Join(dir, "gitconfig"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGitInit(dir); err != nil {
		t.Fatal(err)
	}
	driftReport = func(d string) []string { return []string{"détaché : " + d} }
	t.Cleanup(func() { driftReport = nil })
	if code := execute(env, []string{"st"}); code != 0 || errb.String() != "détaché : "+dir+"\n" {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func runGitInit(dir string) error {
	return gitCmd(dir, "init", "-q").Run()
}

func TestStatusAndPushScope(t *testing.T) {
	env, out, errb := testEnv(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(env.Home, "gitconfig"))
	writeRegistry(t, env, `{"default":"a","profiles":{"a":{"repo":"https://example.com/a.git"},"b":{"repo":"https://example.com/b.git"}}}`)
	for _, k := range []string{"a", "b"} {
		if err := os.MkdirAll(env.ProfileDirFor(k), 0o755); err != nil || runGitInit(env.ProfileDirFor(k)) != nil {
			t.Fatal(err)
		}
	}
	driftReport = func(d string) []string { return []string{"détaché : " + filepath.Base(d)} }
	t.Cleanup(func() { driftReport = nil })
	if code := execute(env, []string{"status"}); code != 0 || !strings.Contains(out.String(), "==> a\n") ||
		!strings.Contains(out.String(), "==> b\n") || errb.String() != "détaché : a\ndétaché : b\n" {
		t.Fatalf("tous les profils : code %d, %q, %q", code, out, errb)
	}
	out.Reset()
	errb.Reset()
	if code := execute(env, []string{"-p", "b", "st"}); code != 0 || strings.Contains(out.String(), "==> ") || errb.String() != "détaché : b\n" {
		t.Fatalf("un profil : code %d, %q, %q", code, out, errb)
	}
	// Two empty clones have nothing to send; a clone that is no repository fails alone, and is named.
	out.Reset()
	errb.Reset()
	if code := execute(env, []string{"push", "msg"}); code != 0 || strings.Count(out.String(), "rien à envoyer") != 2 {
		t.Fatalf("push : code %d, %q, %q", code, out, errb)
	}
	if err := os.RemoveAll(filepath.Join(env.ProfileDirFor("a"), ".git")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errb.Reset()
	if code := execute(env, []string{"push", "msg"}); code != 1 || strings.Count(out.String(), "rien à envoyer") != 1 ||
		!strings.HasSuffix(errb.String(), "échec : a\n") {
		t.Fatalf("push en échec : code %d, %q, %q", code, out, errb)
	}
}
