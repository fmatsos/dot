package doctor

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// marketplace is one declared marketplace; profiles that share a name are merged.
type marketplace struct {
	name    string
	plugins []string
}

func marketplaces(sts []*state) []marketplace {
	var out []marketplace
	for _, s := range sts {
		if s.m == nil || s.m.Marketplace == nil {
			continue
		}
		mk := s.m.Marketplace
		i := slices.IndexFunc(out, func(m marketplace) bool { return m.name == mk.Name })
		if i < 0 {
			out = append(out, marketplace{name: mk.Name})
			i = len(out) - 1
		}
		for _, p := range mk.Plugins {
			if !slices.Contains(out[i].plugins, p) {
				out[i].plugins = append(out[i].plugins, p)
			}
		}
	}
	return out
}

// marketplaces checks the Claude and Codex copies of every declared marketplace, once.
func (c *checker) marketplaces(sts []*state) {
	mks := marketplaces(sts)
	if _, err := exec.LookPath("claude"); err == nil {
		for _, mk := range mks {
			c.claude(mk)
		}
	}
	if _, err := exec.LookPath("codex"); err == nil {
		for _, mk := range mks {
			c.codex(mk)
		}
	}
}

func (c *checker) claude(mk marketplace) {
	var known map[string]struct {
		InstallLocation string `json:"installLocation"`
	}
	data, _ := os.ReadFile(filepath.Join(c.o.Home, ".claude", "plugins", "known_marketplaces.json"))
	_ = json.Unmarshal(data, &known)
	if loc := known[mk.name].InstallLocation; loc != "" && isDir(loc) {
		c.add(OK, "marketplace Claude %s présent", mk.name)
	} else {
		c.add(Warn, "marketplace Claude %s absent : claude plugin marketplace add <dossier> (jq requis)", mk.name)
	}
}

// codexMarket is the directory `codex plugin marketplace list` gives for name, or "".
func codexMarket(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "plugin", "marketplace", "list")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) > 0 && f[0] == name {
			return strings.TrimLeft(strings.TrimPrefix(strings.TrimLeft(l, " \t"), f[0]), " \t")
		}
	}
	return ""
}

func (c *checker) codex(mk marketplace) {
	market := codexMarket(mk.name)
	if market == "" || !isDir(market) {
		c.add(Warn, "marketplace Codex %s absent : codex plugin marketplace add <dossier>", mk.name)
		return
	}
	c.add(OK, "marketplace Codex %s présent", mk.name)
	home := cmp.Or(os.Getenv("CODEX_HOME"), filepath.Join(c.o.Home, ".codex"))
	for _, plugin := range mk.plugins {
		src, cache := pluginSource(market, plugin), newestCopy(filepath.Join(home, "plugins", "cache", mk.name, plugin))
		if src != "" && cache != "" && isDir(filepath.Join(market, src)) && sameTree(filepath.Join(market, src), cache) {
			c.add(OK, "copie Codex à jour")
		} else {
			c.add(Warn, "copie Codex périmée : dot pull")
		}
	}
}

// pluginSource is the string source of a plugin in the marketplace file, or "".
func pluginSource(market, plugin string) string {
	data, _ := os.ReadFile(filepath.Join(market, ".claude-plugin", "marketplace.json"))
	var doc struct {
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	for _, p := range doc.Plugins {
		var s string
		if p.Name == plugin && json.Unmarshal(p.Source, &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// newestCopy is the most recently modified cache directory that holds the plugin manifest.
func newestCopy(dir string) string {
	entries, _ := os.ReadDir(dir) // sorted by name: on equal times the first one wins
	best, bestTime := "", time.Time{}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if fi, err := os.Stat(filepath.Join(p, ".claude-plugin", "plugin.json")); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if fi, err := os.Stat(p); err == nil && (best == "" || fi.ModTime().After(bestTime)) {
			best, bestTime = p, fi.ModTime()
		}
	}
	return best
}

// sameTree is `diff -rq`: same names, same file contents, links followed.
// ponytail: reads whole files; plugin trees are small.
func sameTree(a, b string) bool {
	ea, errA := os.ReadDir(a)
	eb, errB := os.ReadDir(b)
	if errA != nil || errB != nil || len(ea) != len(eb) {
		return false
	}
	for i := range ea {
		if ea[i].Name() != eb[i].Name() {
			return false
		}
		pa, pb := filepath.Join(a, ea[i].Name()), filepath.Join(b, eb[i].Name())
		ia, errA := os.Stat(pa)
		ib, errB := os.Stat(pb)
		if errA != nil || errB != nil || ia.IsDir() != ib.IsDir() {
			return false
		}
		switch {
		case ia.IsDir():
			if !sameTree(pa, pb) {
				return false
			}
		case ia.Mode().Type() != fs.FileMode(0) || ib.Mode().Type() != fs.FileMode(0):
			return false
		default:
			x, errA := os.ReadFile(pa)
			y, errB := os.ReadFile(pb)
			if errA != nil || errB != nil || !bytes.Equal(x, y) {
				return false
			}
		}
	}
	return true
}
