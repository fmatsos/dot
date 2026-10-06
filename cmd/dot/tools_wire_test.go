package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fmatsos/dot/internal/install"
	"github.com/fmatsos/dot/internal/manifest"
)

// DOTFILES_TOOLS=0 (Skip) must not reach codex, which would change the machine.
func TestInstallToolsSkipNeverCallsCodex(t *testing.T) {
	env, _, _ := testEnv(t)
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "codex.log")
	stub := "#!/bin/sh\necho \"$@\" >>" + log + "\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	req := install.ToolsRequest{Home: env.Home, Skip: true, Profiles: []install.ToolProfile{{
		Key: "a", Dir: t.TempDir(), Manifest: &manifest.Manifest{Marketplace: &manifest.Marketplace{Name: "acme", Plugins: []string{"one"}}},
	}}}
	if err := installTools(env, req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("codex called although tools are skipped")
	}
	req.Skip = false
	if err := installTools(env, req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log); err != nil {
		t.Fatal("control: codex stub never called without Skip")
	}
}
