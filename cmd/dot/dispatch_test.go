package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindExtensionInProfileBin(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	env.Profile = "perso"

	// Create an extension in perso/bin.
	binDir := filepath.Join(env.ProfileDirFor("perso"), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(binDir, "dot-hello")
	if err := os.WriteFile(ext, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, deploy, err := findExtension(env, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != ext {
		t.Errorf("path = %q, want %q", path, ext)
	}
	if deploy != env.ProfileDirFor("perso") {
		t.Errorf("deploy = %q, want %q", deploy, env.ProfileDirFor("perso"))
	}
}

func TestFindExtensionInPath(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	env.Profile = "perso"

	// Create a PATH extension (pathbin directory will be in PATH via test setup).
	pathbin := filepath.Join(env.Home, "pathbin")
	if err := os.MkdirAll(pathbin, 0o755); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(pathbin, "dot-world")
	if err := os.WriteFile(ext, []byte("#!/bin/sh\necho world\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathbin+":"+os.Getenv("PATH"))

	path, deploy, err := findExtension(env, "world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != ext {
		t.Errorf("path = %q, want %q", path, ext)
	}
	// With explicit -p, deploy should be set to the profile dir even for PATH extensions.
	if deploy != env.ProfileDirFor("perso") {
		t.Errorf("deploy = %q, want %q", deploy, env.ProfileDirFor("perso"))
	}
}

func TestFindExtensionNotFound(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	env.Profile = "perso"

	path, deploy, err := findExtension(env, "notfound")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
	// deploy should be the profile dir (from ProfileDir() attempt)
	if deploy == "" {
		t.Error("deploy should be non-empty when ProfileDir succeeds")
	}
}

func TestFindExtensionExactlyOne(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	// No explicit profile.

	// Create an extension only in acmecorp/bin.
	binDir := filepath.Join(env.ProfileDirFor("acmecorp"), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(binDir, "dot-acme-tool")
	if err := os.WriteFile(ext, []byte("#!/bin/sh\necho acme\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, deploy, err := findExtension(env, "acme-tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != ext {
		t.Errorf("path = %q, want %q", path, ext)
	}
	if deploy != env.ProfileDirFor("acmecorp") {
		t.Errorf("deploy = %q, want %q", deploy, env.ProfileDirFor("acmecorp"))
	}
}

func TestFindExtensionMultiple(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	// No explicit profile.

	// Create the same extension in both profiles.
	for _, key := range []string{"perso", "acmecorp"} {
		binDir := filepath.Join(env.ProfileDirFor(key), "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		ext := filepath.Join(binDir, "dot-shared")
		if err := os.WriteFile(ext, []byte("#!/bin/sh\necho "+key+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	path, deploy, err := findExtension(env, "shared")
	if err == nil {
		t.Errorf("expected error, got nil; path=%q, deploy=%q", path, deploy)
	}
	if path != "" {
		t.Errorf("path should be empty on error, got %q", path)
	}
	if deploy != "" {
		t.Errorf("deploy should be empty on error, got %q", deploy)
	}
	if err != nil && !strings.Contains(err.Error(), "plusieurs profils") {
		t.Errorf("error should mention multiple profiles: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "perso") {
		t.Errorf("error should mention perso profile: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "acmecorp") {
		t.Errorf("error should mention acmecorp profile: %v", err)
	}
}

func TestFindExtensionWithExplicitProfile(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	env.Profile = "perso" // Explicit profile.

	// Create an extension in both profiles.
	for _, key := range []string{"perso", "acmecorp"} {
		binDir := filepath.Join(env.ProfileDirFor(key), "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		ext := filepath.Join(binDir, "dot-both")
		if err := os.WriteFile(ext, []byte("#!/bin/sh\necho "+key+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// With explicit -p, should only look in perso.
	path, deploy, err := findExtension(env, "both")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != filepath.Join(env.ProfileDirFor("perso"), "bin", "dot-both") {
		t.Errorf("path = %q, want the perso extension", path)
	}
	if deploy != env.ProfileDirFor("perso") {
		t.Errorf("deploy = %q, want perso", deploy)
	}
}

func TestFindExtensionWithDOTFILES_DEPLOY(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	// Don't set env.Profile; set env var instead to test single-profile behavior.
	t.Setenv("DOTFILES_DEPLOY", env.ProfileDirFor("acmecorp"))

	// Create an extension in both profiles.
	for _, key := range []string{"perso", "acmecorp"} {
		binDir := filepath.Join(env.ProfileDirFor(key), "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		ext := filepath.Join(binDir, "dot-deploy")
		if err := os.WriteFile(ext, []byte("#!/bin/sh\necho "+key+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// With DOTFILES_DEPLOY set, should only look in acmecorp (despite multiple profiles).
	path, deploy, err := findExtension(env, "deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(env.ProfileDirFor("acmecorp"), "bin", "dot-deploy")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
	if deploy != env.ProfileDirFor("acmecorp") {
		t.Errorf("deploy = %q, want acmecorp", deploy)
	}
}

func TestFindExtensionInvalidName(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)

	path, deploy, err := findExtension(env, "-invalid-name-start")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" || deploy != "" {
		t.Errorf("invalid name should return empty: path=%q, deploy=%q", path, deploy)
	}
}

func TestFindExtensionNotExecutable(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	env.Profile = "perso"

	// Create a non-executable extension.
	binDir := filepath.Join(env.ProfileDirFor("perso"), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(binDir, "dot-notexec")
	if err := os.WriteFile(ext, []byte("#!/bin/sh\necho notexec\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, deploy, err := findExtension(env, "notexec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("non-executable should not be found: path=%q", path)
	}
	// deploy should still be set to the profile (from ProfileDir())
	if deploy == "" {
		t.Error("deploy should be non-empty")
	}
}
