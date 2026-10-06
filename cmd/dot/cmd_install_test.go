package main

import (
	"strings"
	"testing"
)

func TestInstallWithoutProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"install"}); code != 1 || errb.String() != "dot : aucun profil inscrit (dot install <url>)\n" {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestInstallUsage(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"install", "a", "b"}); code != 2 || !strings.HasPrefix(errb.String(), "usage : dot install") {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestUninstallNeedsExplicitProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	t.Setenv("DOT_PROFILE", "perso") // the environment is not enough for a destructive command
	if code := execute(env, []string{"uninstall"}); code != 2 || errb.String() != "usage : dot uninstall -p <clé> [--purge]\n" {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestUninstallUnknownProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	if code := execute(env, []string{"uninstall", "-p", "nope"}); code != 1 || !strings.Contains(errb.String(), "profil inconnu : nope (profils inscrits : perso, acmecorp)") {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestPullWithoutProfile(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"pull"}); code != 1 || !strings.Contains(errb.String(), "aucun profil inscrit") {
		t.Fatalf("code %d, %q", code, errb)
	}
}
