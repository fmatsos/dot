package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretsEnv points DOTFILES_DEPLOY at a temp profile and puts a fictional pass-cli first in PATH.
func secretsEnv(t *testing.T, mapping string) (env *Env, out, errb *bytes.Buffer) {
	t.Helper()
	env, out, errb = testEnv(t)
	dir := t.TempDir()
	t.Setenv("DOTFILES_DEPLOY", dir)
	t.Setenv("BW_SESSION", "")
	bin := t.TempDir()
	stub := "#!/bin/sh\nprintf 'fake-value\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "pass-cli"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if mapping != "" {
		if err := os.WriteFile(filepath.Join(dir, "secrets.local"), []byte(mapping), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return env, out, errb
}

// swapExec replaces execProcess for one test.
func swapExec(t *testing.T, f func(string, []string, []string) error) {
	t.Helper()
	old := execProcess
	execProcess = f
	t.Cleanup(func() { execProcess = old })
}

func TestSecretsHelpAndUsage(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := execute(env, []string{"secrets"}); code != 0 || !strings.Contains(out.String(), "dot secrets — lit les secrets") {
		t.Fatalf("code %d, %q", code, out)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"secrets", "get"}, "usage : dot secrets get NOM\n"},
		{[]string{"secrets", "add", "A"}, "usage : dot secrets add NOM <réf>\n"},
		{[]string{"secrets", "run", "A"}, "usage : dot secrets run NOM... -- CMD...\n"},
		{[]string{"secrets", "run", "--", "true"}, "usage : dot secrets run NOM... -- CMD...\n"},
		{[]string{"secrets", "run", "A", "--"}, "usage : dot secrets run NOM... -- CMD...\n"},
		{[]string{"secrets", "status", "x"}, "usage : dot secrets status\n"},
		{[]string{"secrets", "nope"}, "dot : commande inconnue : secrets nope\n"},
	} {
		env, _, errb := testEnv(t)
		if code := execute(env, tc.args); code != 2 || errb.String() != tc.want {
			t.Errorf("%v : code %d, stderr %q", tc.args, code, errb)
		}
	}
}

func TestSecretsRunReplacesProcessWithOnlyThoseVariables(t *testing.T) {
	env, _, _ := secretsEnv(t, "# fictional\nACME_TOKEN=pass:Vault/Item\nOTHER=pass:Vault/Other\n")
	t.Setenv("ACME_TOKEN", "parent-only")
	var gotPath string
	var gotArgv, gotEnv []string
	swapExec(t, func(path string, argv, envv []string) error {
		gotPath, gotArgv, gotEnv = path, argv, envv
		return nil
	})
	code := execute(env, []string{"secrets", "run", "ACME_TOKEN", "--", "sh", "-c", "exit 42"})
	if filepath.Base(gotPath) != "sh" || strings.Join(gotArgv, " ") != "sh -c exit 42" {
		t.Fatalf("exec %q %v (code %d)", gotPath, gotArgv, code)
	}
	var tokens []string
	for _, e := range gotEnv {
		if strings.HasPrefix(e, "ACME_TOKEN=") {
			tokens = append(tokens, e)
		}
		if strings.HasPrefix(e, "OTHER=") {
			t.Errorf("unrequested secret exported: %s", e)
		}
	}
	if len(tokens) != 1 || tokens[0] != "ACME_TOKEN=fake-value" {
		t.Errorf("ACME_TOKEN entries = %v", tokens)
	}
}

func TestSecretsRunResolvesBeforeLaunching(t *testing.T) {
	env, _, errb := secretsEnv(t, "ACME_TOKEN=pass:Vault/Item\n")
	called := false
	swapExec(t, func(string, []string, []string) error { called = true; return nil })
	if code := execute(env, []string{"secrets", "run", "ACME_TOKEN", "UNKNOWN", "--", "sh"}); code != 1 || called ||
		errb.String() != "secret : UNKNOWN : NAME inconnu\n" {
		t.Errorf("code %d, called %v, stderr %q", code, called, errb)
	}
	errb.Reset()
	if code := execute(env, []string{"secrets", "run", "ACME_TOKEN", "--", "no-such-command-acmecorp"}); code != 127 || called ||
		errb.String() != "dot : no-such-command-acmecorp : commande introuvable\n" {
		t.Errorf("code %d, called %v, stderr %q", code, called, errb)
	}
}

func TestSecretsErrorsAndExitCodes(t *testing.T) {
	env, _, errb := secretsEnv(t, "ACME_TOKEN=bw:Chat login\nbroken line\n")
	dir := os.Getenv("DOTFILES_DEPLOY")
	if code := execute(env, []string{"secrets", "get", "ACME_TOKEN"}); code != 1 || errb.String() != "secret : ligne 2 mal formée\n" {
		t.Errorf("code %d, stderr %q", code, errb)
	}
	errb.Reset()
	os.WriteFile(filepath.Join(dir, "secrets.local"), []byte("ACME_TOKEN=bw:Chat login\n"), 0o600)
	if code := execute(env, []string{"secrets", "get", "ACME_TOKEN"}); code != 3 || errb.String() != "Bitwarden verrouillé : lance « dot secrets unlock »\n" {
		t.Errorf("locked: code %d, stderr %q", code, errb)
	}
	errb.Reset()
	os.Remove(filepath.Join(dir, "secrets.local"))
	if code := execute(env, []string{"secrets", "status"}); code != 1 || errb.String() != "secret : fichier secrets.local absent ou illisible\n" {
		t.Errorf("absent: code %d, stderr %q", code, errb)
	}
}

func TestSecretsStatusNeverPrintsValues(t *testing.T) {
	env, out, _ := secretsEnv(t, "ACME_TOKEN=pass:Vault/Item\nLOCKED=bw:Item\n")
	if code := execute(env, []string{"secrets", "status"}); code != 0 ||
		out.String() != "ACME_TOKEN (pass) : lisible\nLOCKED (bw) : verrouillé\n" {
		t.Errorf("code %d, %q", code, out)
	}
}

// The argv generated by dot mcp (`dot -p KEY secrets run NAMES -- cmd`) must read KEY's secrets.local.
func TestSecretsRunHonoursProfileBeforeCommand(t *testing.T) {
	env, _, errb := testEnv(t)
	t.Setenv("DOTFILES_DEPLOY", "")
	t.Setenv("DOT_PROFILE", "")
	t.Setenv("BW_SESSION", "")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "pass-cli"), []byte("#!/bin/sh\nprintf 'fake-value\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeRegistry(t, env, twoProfiles) // default: perso
	writeProfileFile(t, env, "acmecorp", "secrets.local", "ACME_TOKEN=pass:Vault/Item\n")
	called := false
	swapExec(t, func(string, []string, []string) error { called = true; return nil })
	if code := execute(env, []string{"-p", "acmecorp", "secrets", "run", "ACME_TOKEN", "--", "sh"}); !called {
		t.Fatalf("-p acmecorp: code %d, %q", code, errb)
	}
	called = false
	errb.Reset()
	writeProfileFile(t, env, "perso", "secrets.local", "PERSO_TOKEN=pass:Vault/Other\n")
	if code := execute(env, []string{"secrets", "run", "ACME_TOKEN", "--", "sh"}); code != 1 || called ||
		errb.String() != "secret : ACME_TOKEN : NAME inconnu\n" {
		t.Fatalf("default profile must not know ACME_TOKEN: code %d, called %v, %q", code, called, errb)
	}
}
