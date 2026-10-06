package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv returns an Env on a temporary HOME with the profile variables cleared,
// plus its captured stdout and stderr. Shared by every cmd_*_test.go file.
func testEnv(t *testing.T) (*Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DOTFILES_DEPLOY", "")
	t.Setenv("DOT_PROFILE", "")
	var out, errb bytes.Buffer
	return &Env{Home: home, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb}, &out, &errb
}

// writeRegistry writes ~/.dot/profiles.json under the env's HOME.
func writeRegistry(t *testing.T, env *Env, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(env.RegistryPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.RegistryPath(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const twoProfiles = `{"default":"perso","profiles":{"perso":{"repo":"https://example.com/p.git"},"acmecorp":{"repo":"https://example.com/a.git"}}}`

func TestProfileDirResolution(t *testing.T) {
	env, _, _ := testEnv(t)
	if _, err := env.ProfileDir(); err == nil || !strings.Contains(err.Error(), "aucun profil inscrit") {
		t.Fatalf("registre vide : err = %v", err)
	}
	writeRegistry(t, env, twoProfiles)
	want := func(key string) string { return filepath.Join(env.Home, ".dot", key) }
	check := func(label, key string) {
		t.Helper()
		got, err := env.ProfileDir()
		if err != nil || got != want(key) {
			t.Fatalf("%s : %q, %v (attendu %q)", label, got, err, want(key))
		}
	}
	check("défaut", "perso")
	t.Setenv("DOT_PROFILE", "acmecorp")
	check("DOT_PROFILE", "acmecorp")
	env.Profile = "perso"
	check("-p l'emporte", "perso")
	env.Profile = "ghost"
	if _, err := env.ProfileDir(); err == nil || !strings.Contains(err.Error(), "profil inconnu : ghost") {
		t.Fatalf("clé inconnue : err = %v", err)
	}
}

func TestProfileDirDeployOverride(t *testing.T) {
	env, _, _ := testEnv(t)
	t.Setenv("DOTFILES_DEPLOY", "~/clone")
	got, err := env.ProfileDir()
	if err != nil || got != filepath.Join(env.Home, "clone") {
		t.Fatalf("got %q, %v", got, err)
	}
	t.Setenv("DOTFILES_DEPLOY", "/abs/clone")
	if got, _ := env.ProfileDir(); got != "/abs/clone" {
		t.Fatalf("got %q", got)
	}
}

func TestProfileDirInvalidRegistry(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, `{"nope":1}`)
	if _, err := env.ProfileDir(); err == nil {
		t.Fatal("registre invalide accepté")
	}
}

func TestProfileKeysOrder(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	keys, err := env.ProfileKeys()
	if err != nil || strings.Join(keys, ",") != "perso,acmecorp" {
		t.Fatalf("keys = %v, %v", keys, err)
	}
}

func TestExpand(t *testing.T) {
	env := &Env{Home: "/h"}
	for in, want := range map[string]string{"~": "/h", "~/a/b": "/h/a/b", "/x": "/x", "a/b": "a/b", "~x": "~x"} {
		if got := env.Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}
