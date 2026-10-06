package main

import (
	"strings"
	"testing"
)

func TestVersionAndHelp(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := execute(env, []string{"--version"}); code != 0 || out.String() != "dot "+version+"\n" {
		t.Fatalf("code %d, sortie %q", code, out)
	}
	out.Reset()
	if code := execute(env, []string{"--help"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"dot installe et entretient un ou plusieurs profils de dotfiles", "--profile", "config"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("aide sans %q :\n%s", want, out)
		}
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"nope"}, "dot : commande inconnue : nope\n"},
		{[]string{"--nope"}, "dot : unknown flag: --nope\n"},
		{[]string{"config", "nope"}, "dot : commande inconnue : config nope\n"},
		{[]string{"config", "get"}, "usage : dot config get <clé>\n"},
	} {
		env, out, errb := testEnv(t)
		if code := execute(env, tc.args); code != 2 || errb.String() != tc.want || out.Len() != 0 {
			t.Errorf("%v : code %d, stderr %q, stdout %q", tc.args, code, errb, out)
		}
	}
}

func TestRuntimeErrorExitsOne(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"config", "get", "profiles.ghost.repo"}); code != 1 || errb.String() != "dot : profil inconnu : ghost\n" {
		t.Fatalf("code %d, stderr %q", code, errb)
	}
}

func TestProfileFlagIsGlobal(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	root := newRoot(env)
	root.SetArgs([]string{"-p", "acmecorp", "config", "list"})
	if err := root.Execute(); err != nil || env.Profile != "acmecorp" {
		t.Fatalf("Profile = %q, err = %v", env.Profile, err)
	}
}

func TestCompletionEnabled(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := execute(env, []string{"completion", "zsh"}); code != 0 || !strings.Contains(out.String(), "#compdef dot") {
		t.Fatalf("code %d", code)
	}
}
