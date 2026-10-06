package main

import (
	"os"
	"strings"
	"testing"
)

func TestConfigListAndGet(t *testing.T) {
	env, out, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	if code := execute(env, []string{"config", "list"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	want := "{\n  \"default\": \"perso\",\n  \"profiles\": {\n    \"perso\": {\n      \"repo\": \"https://example.com/p.git\"\n    },\n    \"acmecorp\": {\n      \"repo\": \"https://example.com/a.git\"\n    }\n  }\n}\n"
	if out.String() != want {
		t.Fatalf("list =\n%s", out)
	}
	for key, val := range map[string]string{"default": "perso", "profiles.acmecorp.repo": "https://example.com/a.git"} {
		out.Reset()
		if code := execute(env, []string{"config", "get", key}); code != 0 || out.String() != val+"\n" {
			t.Fatalf("get %s : code %d, %q", key, code, out)
		}
	}
}

func TestConfigListEmptyRegistry(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := execute(env, []string{"config", "list"}); code != 0 || out.String() != "{\n  \"profiles\": {}\n}\n" {
		t.Fatalf("code %d, %q", code, out)
	}
}

func TestConfigGetUnknownKeyIsUsage(t *testing.T) {
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"config", "get", "nope"}); code != 2 || !strings.HasPrefix(errb.String(), "dot : clé inconnue : nope") {
		t.Fatalf("code %d, %q", code, errb)
	}
}

func TestConfigSetCreatesRegistryAtomically(t *testing.T) {
	env, _, _ := testEnv(t)
	if code := execute(env, []string{"config", "set", "profiles.perso.repo", "https://example.com/p.git"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	r, err := env.Registry()
	if err != nil || r.Default != "perso" || len(r.Profiles) != 1 {
		t.Fatalf("registre = %+v, %v", r, err)
	}
	fi, _ := os.Stat(env.RegistryPath())
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	if code := execute(env, []string{"config", "set", "profiles.acmecorp.repo", "https://example.com/a.git"}); code != 0 {
		t.Fatal("ajout refusé")
	}
	if code := execute(env, []string{"config", "set", "default", "acmecorp"}); code != 0 {
		t.Fatal("default refusé")
	}
	if r, _ := env.Registry(); r.Default != "acmecorp" || strings.Join(r.Keys(), ",") != "perso,acmecorp" {
		t.Fatalf("registre = %+v", r)
	}
}

func TestConfigSetRejectsInvalid(t *testing.T) {
	env, _, errb := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	before, _ := os.ReadFile(env.RegistryPath())
	for _, args := range [][]string{
		{"default", "ghost"},
		{"profiles.bad key.repo", "x"},
		{"profiles.perso.repo", ""},
		{"profiles.perso.repo", "a\nb"},
		{"profiles.perso.name", "x"},
	} {
		errb.Reset()
		if code := execute(env, append([]string{"config", "set"}, args...)); code == 0 {
			t.Errorf("%v accepté", args)
		}
	}
	if after, _ := os.ReadFile(env.RegistryPath()); string(before) != string(after) {
		t.Fatal("registre modifié par un refus")
	}
}

func TestConfigUnset(t *testing.T) {
	env, _, errb := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	if code := execute(env, []string{"config", "unset", "profiles.perso.repo"}); code != 1 || !strings.Contains(errb.String(), "profil par défaut") {
		t.Fatalf("défaut retiré : code %d, %q", code, errb)
	}
	if err := os.MkdirAll(env.ProfileDirFor("acmecorp"), 0o700); err != nil {
		t.Fatal(err)
	}
	errb.Reset()
	if code := execute(env, []string{"config", "unset", "profiles.acmecorp.repo"}); code != 1 || !strings.Contains(errb.String(), "encore installé") {
		t.Fatalf("profil installé retiré : code %d, %q", code, errb)
	}
	if err := os.Remove(env.ProfileDirFor("acmecorp")); err != nil {
		t.Fatal(err)
	}
	if code := execute(env, []string{"config", "unset", "profiles.acmecorp.repo"}); code != 0 {
		t.Fatal("retrait refusé")
	}
	if r, _ := env.Registry(); strings.Join(r.Keys(), ",") != "perso" {
		t.Fatalf("registre = %+v", r)
	}
	if code := execute(env, []string{"config", "unset", "default"}); code != 1 {
		t.Fatal("default retiré avec un profil")
	}
	if code := execute(env, []string{"config", "unset", "profiles.perso.repo"}); code != 0 {
		t.Fatal("dernier profil non retiré")
	}
	if r, _ := env.Registry(); r.Default != "" || len(r.Profiles) != 0 {
		t.Fatalf("registre = %+v", r)
	}
}

func TestConfigRefusesInvalidRegistry(t *testing.T) {
	env, _, errb := testEnv(t)
	writeRegistry(t, env, `{"profiles":{"a":{"repo":""}}}`)
	if code := execute(env, []string{"config", "list"}); code != 1 || !strings.HasPrefix(errb.String(), "dot : registre : ") {
		t.Fatalf("code %d, %q", code, errb)
	}
}
