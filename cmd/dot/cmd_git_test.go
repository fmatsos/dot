package main

import (
	"os"
	"os/exec"
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
			t.Errorf("route(%v) = %v, %v, profile %q", tc.args, rest, ext, env.Profile)
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
		if code := execute(env, args); code != 2 {
			t.Errorf("%v: code %d, %q", args, code, errb)
		}
	}
	out.Reset()
	// The help text is a message: only the exit code and the presence of a text are checked.
	if code := execute(env, []string{"clone", "--help"}); code != 0 || out.Len() == 0 {
		t.Errorf("clone --help: code %d, %q", code, out)
	}
}

func TestHelpUnknownTopicIsUsage(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"help", "nope"}); code != 2 {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestProfileRuleIsIdempotent(t *testing.T) {
	env, _, errb := testEnv(t)
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
	want := "[includeIf \"gitdir:" + proj + "/\"]\n\tpath = ~/.config/git/profiles/perso.local\n"
	for i := range 2 {
		if code := execute(env, []string{"profile", "perso", proj}); code != 0 {
			t.Fatalf("pass %d: code %d, %q", i, code, errb)
		}
		// The rule is written once: the second pass leaves the file as it is.
		got, _ := os.ReadFile(filepath.Join(env.Home, ".config/git/profiles.local"))
		if string(got) != want {
			t.Fatalf("pass %d: profiles.local = %q", i, got)
		}
	}
	// Another profile on the same target must not be reported as present: the rule keeps the first.
	if err := os.WriteFile(filepath.Join(prof, "other.local"), []byte("# fictional\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errb.Reset()
	if code := execute(env, []string{"profile", "other", proj}); code != 1 {
		t.Fatalf("other profile: code %d, %q", code, errb)
	}
	if again, _ := os.ReadFile(filepath.Join(env.Home, ".config/git/profiles.local")); string(again) != want {
		t.Fatalf("profiles.local modified: %q", again)
	}
	errb.Reset()
	if code := execute(env, []string{"profile", "a..b", proj}); code != 1 {
		t.Fatalf("invalid name: code %d, %q", code, errb)
	}
	if again, _ := os.ReadFile(filepath.Join(env.Home, ".config/git/profiles.local")); string(again) != want {
		t.Fatalf("profiles.local modified by an invalid name: %q", again)
	}
	if code := execute(env, []string{"profile", "perso", "git@example.com:me/**"}); code != 0 {
		t.Fatalf("remote: code %d, %q", code, errb)
	}
	if rules, _ := os.ReadFile(filepath.Join(env.Home, ".config/git/profiles.local")); !strings.Contains(string(rules), `[includeIf "hasconfig:remote.*.url:git@example.com:me/**"]`) {
		t.Fatalf("remote rule missing from profiles.local: %q", rules)
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
	// The stub's own text, relayed as it is: not dot's wording.
	driftReport = func(d string) []string { return []string{"DRIFT " + d} }
	t.Cleanup(func() { driftReport = nil })
	if code := execute(env, []string{"st"}); code != 0 || !strings.Contains(errb.String(), "DRIFT "+dir+"\n") {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func runGitInit(dir string) error {
	return gitCmd(dir, "init", "-q").Run()
}

func TestStatusAndPushScope(t *testing.T) {
	env, out, errb := testEnv(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(env.Home, "gitconfig"))
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	writeRegistry(t, env, `{"default":"a","profiles":{"a":{"repo":"https://example.com/a.git"},"b":{"repo":"https://example.com/b.git"}}}`)
	for _, k := range []string{"a", "b"} {
		if err := os.MkdirAll(env.ProfileDirFor(k), 0o755); err != nil || runGitInit(env.ProfileDirFor(k)) != nil {
			t.Fatal(err)
		}
	}
	// status asks for the drift of the targeted clones only, in registry order.
	var drifted []string
	driftReport = func(d string) []string { drifted = append(drifted, filepath.Base(d)); return nil }
	t.Cleanup(func() { driftReport = nil })
	if code := execute(env, []string{"status"}); code != 0 || strings.Join(drifted, ",") != "a,b" {
		t.Fatalf("all profiles: code %d, drift %v, %q, %q", code, drifted, out, errb)
	}
	drifted = nil
	if code := execute(env, []string{"-p", "b", "st"}); code != 0 || strings.Join(drifted, ",") != "b" {
		t.Fatalf("one profile: code %d, drift %v, %q, %q", code, drifted, out, errb)
	}
	// b has a remote and a tracked change; a is an empty clone with nothing to send. A clone that
	// is no repository fails alone: the others are still pushed, observed in the remote.
	remote := t.TempDir()
	b := env.ProfileDirFor("b")
	for _, args := range [][]string{{"init", "-q", "--bare", remote}, {"-C", b, "remote", "add", "origin", remote}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(b, "f"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "f"}, {"commit", "-q", "-m", "init"}, {"push", "-q", "-u", "origin", "HEAD"}} {
		if out, err := gitCmd(b, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	pushed := func() []string {
		t.Helper()
		out, err := exec.Command("git", "-C", remote, "log", "--all", "--format=%s").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Fields(string(out))
	}
	if err := os.WriteFile(filepath.Join(b, "f"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := execute(env, []string{"push", "msg"}); code != 0 || strings.Join(pushed(), ",") != "msg,init" {
		t.Fatalf("push: code %d, remote %v, %q, %q", code, pushed(), out, errb)
	}
	if _, err := gitCmd(env.ProfileDirFor("a"), "rev-parse", "--verify", "-q", "HEAD").Output(); err == nil {
		t.Fatal("nothing to send for a: a commit was created anyway")
	}
	if err := os.RemoveAll(filepath.Join(env.ProfileDirFor("a"), ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "f"), []byte("3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := execute(env, []string{"push", "msg2"}); code != 1 || strings.Join(pushed(), ",") != "msg2,msg,init" {
		t.Fatalf("failing push: code %d, remote %v, %q, %q", code, pushed(), out, errb)
	}
}
