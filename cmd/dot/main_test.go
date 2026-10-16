package main

import (
	"strings"
	"testing"
)

func TestVersionAndHelp(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := execute(env, []string{"--version"}); code != 0 || out.String() != "dot "+version+"\n" {
		t.Fatalf("code %d, output %q", code, out)
	}
	out.Reset()
	if code := execute(env, []string{"--help"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	if out.Len() == 0 {
		t.Error("--help prints nothing")
	}
	// The help texts themselves are messages; what the help documents is the command tree.
	root := newRoot(env)
	if root.PersistentFlags().Lookup("profile") == nil {
		t.Error("the --profile flag does not exist")
	}
	if cmd, _, err := root.Find([]string{"config"}); err != nil || cmd.Name() != "config" {
		t.Errorf("the config command does not exist: %v", err)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"nope"},
		{"--nope"},
		{"config", "nope"},
		{"config", "get"},
	} {
		env, out, errb := testEnv(t)
		if code := execute(env, args); code != 2 || out.Len() != 0 {
			t.Errorf("%v: code %d, stderr %q, stdout %q", args, code, errb, out)
		}
	}
}

func TestRuntimeErrorExitsOne(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"config", "get", "profiles.ghost.repo"}); code != 1 {
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
