package main

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// The forge call fails on purpose: only what is asked and what reaches the CLI matter here.
func TestReposCreateAsksOnlyMissing(t *testing.T) {
	oldPrompt, oldForge := promptVisible, forgeOutput
	t.Cleanup(func() { promptVisible, forgeOutput = oldPrompt, oldForge })
	for _, tc := range []struct {
		args, answers []string
		asked         int
		call          string
	}{
		{nil, []string{"demo", "bitbucket", "gitlab", "internal"}, 4, "glab repo create demo --internal"},
		{[]string{"demo", "--provider", "github"}, []string{"internal", "public"}, 2, "gh repo create demo --public"},
		{[]string{"demo", "--provider", "github", "--visibility", "private", "--owner", "acme"}, nil, 0, "gh repo create acme/demo --private"},
	} {
		env, _, _ := testEnv(t)
		answers := slices.Clone(tc.answers)
		asked := 0
		promptVisible = func(string) (string, error) {
			asked++
			if len(answers) == 0 {
				return "", errors.New("no tty")
			}
			a := answers[0]
			answers = answers[1:]
			return a, nil
		}
		var call string
		forgeOutput = func(_ *Env, name string, args ...string) (string, error) {
			call = name + " " + strings.Join(args, " ")
			return "", errors.New("offline")
		}
		code := execute(env, append([]string{"repos", "create"}, tc.args...))
		if code == 0 || asked != tc.asked || call != tc.call {
			t.Errorf("%v: code %d, asked %d, call %q", tc.args, code, asked, call)
		}
	}
}

func TestReposCreateWithoutTerminal(t *testing.T) {
	env, _, _ := testEnv(t)
	old := promptVisible
	t.Cleanup(func() { promptVisible = old })
	promptVisible = func(string) (string, error) { return "", errors.New("no tty") }
	if code := execute(env, []string{"repos", "create", "demo"}); code != 2 {
		t.Fatalf("code %d, want the usage code 2", code)
	}
}
