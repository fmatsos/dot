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
	for _, args := range [][]string{{"ai", "mcp", "-n"}, {"-p", "perso", "ai", "mcp", "-n"}} {
		errb.Reset()
		if code := execute(env, args); code != 1 {
			t.Fatalf("%v must refuse the other profile's secret: code %d, %q", args, code, errb)
		}
	}
	writeProfileFile(t, env, "perso", "secrets.local", "ACME_TOKEN=bw:PERSO\n")
	if code := execute(env, []string{"ai", "mcp"}); code != 0 || out.Len() != 0 {
		t.Fatalf("code %d, %q, %q", code, out, errb)
	}
}

func TestMcpErrorsAndUsage(t *testing.T) {
	noTools(t)
	env, _, errb := testEnv(t)
	if code := execute(env, []string{"ai", "mcp"}); code != 1 {
		t.Fatalf("empty registry: code %d, %q", code, errb)
	}
	writeRegistry(t, env, twoProfiles)
	errb.Reset()
	if code := execute(env, []string{"ai", "mcp"}); code != 1 {
		t.Fatalf("code %d, %q", code, errb)
	}
	if code := execute(env, []string{"ai", "mcp", "--bad"}); code != 2 {
		t.Fatalf("code %d", code)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".config")); err == nil {
		t.Fatal("an error wrote something")
	}
}

// The former top-level commands point to dot ai instead of running git on the profile clone.
func TestFormerCommandsPointToAi(t *testing.T) {
	env, _, errb := testEnv(t)
	writeRegistry(t, env, twoProfiles)
	for _, args := range [][]string{{"mcp"}, {"settings", "-n"}, {"-p", "perso", "mcp", "-n"}} {
		errb.Reset()
		if code := execute(env, args); code != 2 || !strings.Contains(errb.String(), "moved to dot ai") {
			t.Fatalf("%v: code %d, %q", args, code, errb)
		}
	}
}
