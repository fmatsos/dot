package main

import (
	"os"
	"testing"
)

func TestInstallWithoutProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"install"}); code != 1 {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestInstallUsage(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"install", "a", "b"}); code != 2 {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestUninstallNeedsExplicitProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	t.Setenv("DOT_PROFILE", "perso") // the environment is not enough for a destructive command
	if code := execute(env, []string{"uninstall"}); code != 2 {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestUninstallUnknownProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	before, _ := os.ReadFile(env.RegistryPath())
	if code := execute(env, []string{"uninstall", "-p", "nope"}); code != 1 {
		t.Fatalf("code %d, %q", code, errb)
	}
	if after, _ := os.ReadFile(env.RegistryPath()); string(before) != string(after) {
		t.Fatal("registry modified by a refusal")
	}
}

func TestPullWithoutProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"pull"}); code != 1 {
		t.Fatalf("code %d, %q", code, errb)
	}
}
