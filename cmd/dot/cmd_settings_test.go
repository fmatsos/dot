package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfileFile(t *testing.T, env *Env, key, rel, content string) {
	t.Helper()
	p := filepath.Join(env.ProfileDirFor(key), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsMergesEveryProfileInRegistryOrder(t *testing.T) {
	env, out, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	writeProfileFile(t, env, "perso", "home/.claude/settings.base.json", `{"theme":"perso","a":1}`)
	writeProfileFile(t, env, "acmecorp", "home/.claude/settings.base.json", `{"theme":"acme"}`)
	if code := execute(env, []string{"settings"}); code != 0 {
		t.Fatalf("code %d, %q", code, out)
	}
	got, _ := os.ReadFile(filepath.Join(env.Home, ".claude", "settings.json"))
	if string(got) != "{\n  \"a\": 1,\n  \"theme\": \"acme\"\n}\n" {
		t.Fatalf("settings.json = %s", got)
	}
	out.Reset()
	if code := execute(env, []string{"settings"}); code != 0 || out.String() != "settings: à jour\n" {
		t.Fatalf("code %d, %q", code, out)
	}
}

func TestSettingsProfileFlagTargetsOne(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	writeProfileFile(t, env, "perso", "home/.claude/settings.base.json", `{"theme":"perso"}`)
	writeProfileFile(t, env, "acmecorp", "home/.claude/settings.base.json", `{"theme":"acme"}`)
	if code := execute(env, []string{"-p", "perso", "settings", "-n"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".claude")); err == nil {
		t.Fatal("-n wrote something")
	}
}

func TestSettingsErrors(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"settings"}); code != 1 || !strings.Contains(errb.String(), "aucun profil inscrit") {
		t.Fatalf("registre vide : code %d, %q", code, errb)
	}
	if code := execute(env, []string{"settings", "--invalid"}); code != 2 {
		t.Fatalf("option inconnue : code %d", code)
	}
	if code := execute(env, []string{"settings", "extra"}); code != 2 || !strings.Contains(errb.String(), "usage : dot settings [-n]") {
		t.Fatalf("argument en trop : code %d, %q", code, errb)
	}
}
