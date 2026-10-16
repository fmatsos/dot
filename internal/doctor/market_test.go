package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	if problems(lines) != nil {
		t.Errorf("healthy marketplaces:\n%s", text(lines))
	}
	// message kept: a present marketplace or an up-to-date copy is only an OK line, known by its text (no check id on Line).
	for _, want := range []string{"marketplace Claude acme present", "marketplace Codex acme present"} {
		if !has(lines, OK, want) {
			t.Errorf("missing %q:\n%s", want, text(lines))
		}
	}
	if !has(lines, OK, "Codex copy up to date") {
		t.Errorf("up-to-date copy:\n%s", text(lines))
	}
	write(t, filepath.Join(cache, "example"), "never-print-plugin-content\n", 0o644)
	lines = check(home, p)
	if !slices.Equal(problems(lines), []Level{Warn}) || HasFail(lines) {
		t.Errorf("stale copy:\n%s", text(lines))
	}
	if out := text(lines); strings.Contains(out, "never-print-plugin-content") {
		t.Error("plugin content shown")
	}
	write(t, filepath.Join(cache, "example"), "original\n", 0o644)
	write(t, filepath.Join(cache, "extra"), "x", 0o644)
	if lines = check(home, p); !slices.Equal(problems(lines), []Level{Warn}) {
		t.Errorf("extra file:\n%s", text(lines))
	}
	os.Remove(filepath.Join(cache, "extra"))
	os.RemoveAll(market)
	lines = check(home, p)
	// one warning per CLI whose marketplace is gone, and no failure
	if !slices.Equal(problems(lines), []Level{Warn, Warn}) {
		t.Errorf("missing marketplaces:\n%s", text(lines))
	}
}

func TestMarketplaceDeclaredByTwoProfilesIsCheckedOnce(t *testing.T) {
	home, _, _ := marketSandbox(t)
	lines := check(home, profile(t, home, "alpha"), profile(t, home, "beta"))
	n := 0
	for _, l := range lines {
		if l.Message == "marketplace Claude acme present" || l.Message == "Codex copy up to date" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d marketplace lines, want 2:\n%s", n, text(lines))
	}
}

func TestNoMarketplaceCliSkipsChecks(t *testing.T) {
	home := sandbox(t)
	// message kept: the marketplace checks are only known by their text (no check id on Line).
	if out := text(check(home, profile(t, home, "alpha"))); strings.Contains(out, "marketplace") {
		t.Errorf("marketplace checked without a CLI:\n%s", out)
	}
}
