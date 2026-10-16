package main

import (
	"os"
	"path/filepath"
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
	if code := execute(env, []string{"ai", "settings"}); code != 0 {
		t.Fatalf("code %d, %q", code, out)
	}
	got, _ := os.ReadFile(filepath.Join(env.Home, ".claude", "settings.json"))
	if string(got) != "{\n  \"a\": 1,\n  \"theme\": \"acme\"\n}\n" {
		t.Fatalf("settings.json = %s", got)
	}
	// Second run: nothing to change, the file is left as it is.
	fi, _ := os.Stat(filepath.Join(env.Home, ".claude", "settings.json"))
	if code := execute(env, []string{"ai", "settings"}); code != 0 {
		t.Fatalf("code %d, %q", code, out)
	}
	again, _ := os.ReadFile(filepath.Join(env.Home, ".claude", "settings.json"))
	fi2, _ := os.Stat(filepath.Join(env.Home, ".claude", "settings.json"))
	if string(again) != string(got) || !fi2.ModTime().Equal(fi.ModTime()) {
		t.Fatalf("settings.json rewritten: %s", again)
	}
}

func TestSettingsProfileFlagTargetsOne(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	writeProfileFile(t, env, "perso", "home/.claude/settings.base.json", `{"theme":"perso"}`)
	writeProfileFile(t, env, "acmecorp", "home/.claude/settings.base.json", `{"theme":"acme"}`)
	if code := execute(env, []string{"-p", "perso", "ai", "settings", "-n"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".claude")); err == nil {
		t.Fatal("-n wrote something")
	}
}

func TestSettingsErrors(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"ai", "settings"}); code != 1 {
		t.Fatalf("empty registry: code %d, %q", code, errb)
	}
	if code := execute(env, []string{"ai", "settings", "--invalid"}); code != 2 {
		t.Fatalf("unknown option: code %d", code)
	}
	if code := execute(env, []string{"ai", "settings", "extra"}); code != 2 {
		t.Fatalf("extra argument: code %d, %q", code, errb)
	}
}
