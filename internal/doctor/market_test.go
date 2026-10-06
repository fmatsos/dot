package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// marketSandbox adds fictional claude and codex CLIs and a marketplace with a cached copy.
func marketSandbox(t *testing.T) (home, market, cache string) {
	t.Helper()
	home = sandbox(t)
	bin := filepath.Dir(mustLook(t, "mise"))
	market = filepath.Join(filepath.Dir(home), "plugin market")
	write(t, filepath.Join(bin, "claude"), "#!/bin/sh\nexit 0\n", 0o755)
	write(t, filepath.Join(bin, "codex"), "#!/bin/sh\nprintf 'MARKETPLACE ROOT\\nacme %s\\n' \"$FAKE_MARKET\"\n", 0o755)
	t.Setenv("FAKE_MARKET", market)
	write(t, filepath.Join(market, ".claude-plugin/marketplace.json"), `{"plugins":[{"name":"acme","source":"./plugin"}]}`, 0o644)
	write(t, filepath.Join(market, "plugin/.claude-plugin/plugin.json"), `{"name":"acme"}`, 0o644)
	write(t, filepath.Join(market, "plugin/example"), "original\n", 0o644)
	cache = filepath.Join(home, ".codex/plugins/cache/acme/acme/1.0.0")
	write(t, filepath.Join(cache, ".claude-plugin/plugin.json"), `{"name":"acme"}`, 0o644)
	write(t, filepath.Join(cache, "example"), "original\n", 0o644)
	write(t, filepath.Join(home, ".claude/plugins/known_marketplaces.json"),
		`{"acme":{"installLocation":"`+market+`"}}`, 0o644)
	return home, market, cache
}

func mustLook(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMarketplaces(t *testing.T) {
	home, market, cache := marketSandbox(t)
	p := profile(t, home, "alpha")
	lines := check(home, p)
	for _, want := range []string{"marketplace Claude acme présent", "marketplace Codex acme présent"} {
		if !has(lines, OK, want) {
			t.Errorf("manque %q :\n%s", want, text(lines))
		}
	}
	if !has(lines, OK, "copie Codex à jour") {
		t.Errorf("copie à jour :\n%s", text(lines))
	}
	write(t, filepath.Join(cache, "example"), "never-print-plugin-content\n", 0o644)
	lines = check(home, p)
	if !has(lines, Warn, "copie Codex périmée : dot pull") || HasFail(lines) {
		t.Errorf("copie périmée :\n%s", text(lines))
	}
	if out := text(lines); strings.Contains(out, "never-print-plugin-content") {
		t.Error("contenu du plugin affiché")
	}
	write(t, filepath.Join(cache, "example"), "original\n", 0o644)
	write(t, filepath.Join(cache, "extra"), "x", 0o644)
	if lines = check(home, p); !has(lines, Warn, "copie Codex périmée : dot pull") {
		t.Errorf("fichier en trop :\n%s", text(lines))
	}
	os.Remove(filepath.Join(cache, "extra"))
	os.RemoveAll(market)
	lines = check(home, p)
	for _, l := range []Line{
		{Warn, "marketplace Claude acme absent : claude plugin marketplace add <dossier> (jq requis)"},
		{Warn, "marketplace Codex acme absent : codex plugin marketplace add <dossier>"},
	} {
		if !has(lines, l.Level, l.Message) {
			t.Errorf("manque %q :\n%s", l.Message, text(lines))
		}
	}
}

func TestMarketplaceDeclaredByTwoProfilesIsCheckedOnce(t *testing.T) {
	home, _, _ := marketSandbox(t)
	lines := check(home, profile(t, home, "alpha"), profile(t, home, "beta"))
	n := 0
	for _, l := range lines {
		if l.Message == "marketplace Claude acme présent" || l.Message == "copie Codex à jour" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d lignes de marketplace, attendu 2 :\n%s", n, text(lines))
	}
}

func TestNoMarketplaceCliSkipsChecks(t *testing.T) {
	home := sandbox(t)
	if out := text(check(home, profile(t, home, "alpha"))); strings.Contains(out, "marketplace") {
		t.Errorf("marketplace vérifiée sans CLI :\n%s", out)
	}
}
