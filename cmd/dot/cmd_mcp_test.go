package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noTools points PATH at an empty directory: no claude, codex or opencode is found.
func noTools(t *testing.T) { t.Setenv("PATH", t.TempDir()) }

func TestMcpSecretNamesComeFromTheServerProfile(t *testing.T) {
	noTools(t)
	env, out, errb := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	writeProfileFile(t, env, "perso", "home/.config/mcp/servers.json", `{"a":{"command":"a","secrets":["ACME_TOKEN"]}}`)
	writeProfileFile(t, env, "acmecorp", "secrets.local", "ACME_TOKEN=bw:ACME\n")
	// A name declared only by the other profile is refused, with or without -p.
	for _, args := range [][]string{{"mcp", "-n"}, {"-p", "perso", "mcp", "-n"}} {
		errb.Reset()
		if code := execute(env, args); code != 1 || !strings.Contains(errb.String(), "mcp : source invalide ou NOM de secret inconnu") {
			t.Fatalf("%v must refuse the other profile's secret: code %d, %q", args, code, errb)
		}
	}
	writeProfileFile(t, env, "perso", "secrets.local", "ACME_TOKEN=bw:PERSO\n")
	if code := execute(env, []string{"mcp"}); code != 0 || out.Len() != 0 {
		t.Fatalf("code %d, %q, %q", code, out, errb)
	}
}

func TestMcpErrorsAndUsage(t *testing.T) {
	noTools(t)
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"mcp"}); code != 1 || !strings.Contains(errb.String(), "aucun profil inscrit") {
		t.Fatalf("registre vide : code %d, %q", code, errb)
	}
	writeRegistry(t, env, twoProfiles)
	errb.Reset()
	if code := execute(env, []string{"mcp"}); code != 1 || errb.String() != "mcp : servers.json invalide ou absent\n" {
		t.Fatalf("code %d, %q", code, errb)
	}
	if code := execute(env, []string{"mcp", "--bad"}); code != 2 {
		t.Fatalf("code %d", code)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".config")); err == nil {
		t.Fatal("an error wrote something")
	}
}
