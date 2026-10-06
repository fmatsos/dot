package manifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const valid = `{
  "repo": "https://git.example.com/acme/dots.git",
  "deploy": {"sparse": ["home", "bin", ".githooks"]},
  "profiles": {"demo": "home/.config/git/profiles/demo.gitconfig"},
  "deployProfile": "demo",
  "marketplace": {"name": "acme", "plugins": ["demo"]},
  "nvm": {"node": "1.2.3", "packages": ["demo-cli@1.2.3", "@acme/demo@2.0.0"]},
  "modules": ["settings", "mcp"]
}`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dot.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	m, err := Load(writeFile(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	want := &Manifest{
		Repo:          "https://git.example.com/acme/dots.git",
		Sparse:        []string{"home", "bin", ".githooks"},
		Profiles:      []ProfilePath{{"demo", "home/.config/git/profiles/demo.gitconfig"}},
		DeployProfile: "demo",
		Marketplace:   &Marketplace{Name: "acme", Plugins: []string{"demo"}},
		NVM:           &NVM{Node: "1.2.3", Packages: []string{"demo-cli@1.2.3", "@acme/demo@2.0.0"}},
		Modules:       []string{"settings", "mcp"},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("manifeste = %+v, want %+v", m, want)
	}
}

func TestLoadOptionalBlocksAbsent(t *testing.T) {
	m, err := Load(writeFile(t, `{"repo":"r","deploy":{"sparse":["home"]},"profiles":{"a":"home/a.gitconfig"},"deployProfile":"a","modules":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Marketplace != nil || m.NVM != nil || len(m.Modules) != 0 {
		t.Fatalf("blocs optionnels présents : %+v", m)
	}
}

func TestLoadKeepsProfileOrder(t *testing.T) {
	m, err := Load(writeFile(t, `{"repo":"r","deploy":{"sparse":["home"]},"profiles":{"zeta":"home/z.gitconfig","alpha":"home/a.gitconfig"},"deployProfile":"alpha","modules":["mcp"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Profiles[0].Name != "zeta" || m.Profiles[1].Name != "alpha" {
		t.Fatalf("ordre perdu : %+v", m.Profiles)
	}
}

func TestLoadRejects(t *testing.T) {
	set := func(path ...any) func(map[string]any) {
		return func(m map[string]any) {
			cur := m
			for _, p := range path[:len(path)-2] {
				cur = cur[p.(string)].(map[string]any)
			}
			cur[path[len(path)-2].(string)] = path[len(path)-1]
		}
	}
	del := func(k string) func(map[string]any) { return func(m map[string]any) { delete(m, k) } }
	cases := []struct {
		key string
		mut func(map[string]any)
	}{
		{"unexpected", set("unexpected", true)},
		{"deploy.typo", set("deploy", "typo", true)},
		{"deploy.path", set("deploy", "path", "~/.config/dots")},
		{"marketplace.typo", set("marketplace", "typo", true)},
		{"nvm.typo", set("nvm", "typo", true)},
		{"repo", set("repo", "")},
		{"repo", set("repo", 42)},
		{"repo", del("repo")},
		{"repo", set("repo", "bad\u0000data")},
		{"deploy", set("deploy", []any{})},
		{"deploy", del("deploy")},
		{"deploy.sparse", set("deploy", "sparse", []any{})},
		{"deploy.sparse", set("deploy", "sparse", "home")},
		{"deploy.sparse", set("deploy", "sparse", []any{"/home"})},
		{"deploy.sparse", set("deploy", "sparse", []any{"a..b"})},
		{"deploy.sparse", set("deploy", "sparse", []any{"."})},
		{"deploy.sparse", set("deploy", "sparse", []any{42})},
		{"profiles", set("profiles", []any{})},
		{"profiles", set("profiles", map[string]any{})},
		{"profiles.bad/name", func(m map[string]any) {
			m["profiles"] = map[string]any{"bad/name": "home/demo.gitconfig"}
			m["deployProfile"] = "bad/name"
		}},
		{"profiles.demo", set("profiles", "demo", "other/demo.gitconfig")},
		{"profiles.demo", set("profiles", "demo", "home/demo.txt")},
		{"profiles.demo", set("profiles", "demo", "home/../demo.gitconfig")},
		{"profiles.demo", set("profiles", "demo", 42)},
		{"deployProfile", set("deployProfile", "missing")},
		{"deployProfile", set("deployProfile", 42)},
		{"modules", set("modules", "settings")},
		{"modules", set("modules", []any{"unknown"})},
		{"modules", set("modules", []any{"settings", "settings"})},
		{"modules", del("modules")},
		{"marketplace", set("marketplace", []any{})},
		{"marketplace", set("marketplace", nil)},
		{"marketplace.name", set("marketplace", "name", "-bad")},
		{"marketplace.name", set("marketplace", "name", 42)},
		{"marketplace.plugins", set("marketplace", "plugins", []any{"bad/name"})},
		{"marketplace.plugins", set("marketplace", "plugins", "demo")},
		{"marketplace.plugins", set("marketplace", "plugins", []any{42})},
		{"nvm", set("nvm", []any{})},
		{"nvm", set("nvm", nil)},
		{"nvm.node", set("nvm", "node", "v1.2.3")},
		{"nvm.node", set("nvm", "node", 42)},
		{"nvm.packages", set("nvm", "packages", []any{"demo"})},
		{"nvm.packages", set("nvm", "packages", "demo@1.0.0")},
		{"nvm.packages", set("nvm", "packages", []any{42})},
	}
	for _, c := range cases {
		var m map[string]any
		if err := json.Unmarshal([]byte(valid), &m); err != nil {
			t.Fatal(err)
		}
		c.mut(m)
		data, _ := json.Marshal(m)
		path := writeFile(t, string(data))
		_, err := Load(path)
		var ke *KeyError
		if !errors.As(err, &ke) || ke.Key != c.key {
			t.Errorf("%s : err = %v", c.key, err)
			continue
		}
		if want := "manifest : " + path + " : clé " + c.key + " invalide ou fichier absent"; err.Error() != want {
			t.Errorf("message = %q, want %q", err.Error(), want)
		}
	}
}

func TestLoadRejectsMalformedFiles(t *testing.T) {
	for name, content := range map[string]string{
		"tableau":        `[]`,
		"null":           `null`,
		"scalaire":       `42`,
		"vide":           ``,
		"tronqué":        `{`,
		"deux documents": valid + valid,
		"déchets":        valid + ` x`,
	} {
		_, err := Load(writeFile(t, content))
		var ke *KeyError
		if !errors.As(err, &ke) || ke.Key != "objet" {
			t.Errorf("%s : err = %v", name, err)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dot.json")
	_, err := Load(path)
	var ke *KeyError
	if !errors.As(err, &ke) || ke.Key != "objet" || ke.File != path {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFirstUnknownKeyInFileOrder(t *testing.T) {
	_, err := Load(writeFile(t, `{"zzz":1,"aaa":2}`))
	var ke *KeyError
	if !errors.As(err, &ke) || ke.Key != "zzz" {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadStringsAreData(t *testing.T) {
	m, err := Load(writeFile(t, `{"repo":"$(touch SHOULD_NOT_EXIST)","deploy":{"sparse":["home"]},"profiles":{"a":"home/a.gitconfig"},"deployProfile":"a","modules":[]}`))
	if err != nil || m.Repo != "$(touch SHOULD_NOT_EXIST)" {
		t.Fatalf("m = %+v, err = %v", m, err)
	}
}
