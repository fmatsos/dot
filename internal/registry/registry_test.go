package registry

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "absent", "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Default != "" || len(r.Profiles) != 0 || len(r.Keys()) != 0 {
		t.Fatalf("registre non vide : %+v", r)
	}
}

func TestLoadKeepsFileOrder(t *testing.T) {
	r, err := Load(write(t, `{"default":"work","profiles":{"zeta":{"repo":"https://example.com/z.git"},"work":{"repo":"https://example.com/w.git"},"alpha":{"repo":"https://example.com/a.git"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"zeta", "work", "alpha"}; !reflect.DeepEqual(r.Keys(), want) {
		t.Fatalf("Keys = %v, want %v", r.Keys(), want)
	}
	if r.Default != "work" {
		t.Fatalf("Default = %q", r.Default)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"racine inconnue":     `{"profiles":{},"extra":1}`,
		"champ de profil":     `{"default":"a","profiles":{"a":{"repo":"r","extra":1}}}`,
		"profiles non objet":  `{"profiles":[]}`,
		"profiles null":       `{"profiles":null}`,
		"json invalide":       `{`,
		"vide":                ``,
		"deux documents":      `{"profiles":{}} {"profiles":{}}`,
		"tableau":             `[]`,
		"clé dupliquée":       `{"default":"a","profiles":{"a":{"repo":"r"},"a":{"repo":"s"}}}`,
		"clé invalide":        `{"default":"a/b","profiles":{"a/b":{"repo":"r"}}}`,
		"clé commence par .":  `{"default":".a","profiles":{".a":{"repo":"r"}}}`,
		"clé avec ..":         `{"default":"a..b","profiles":{"a..b":{"repo":"r"}}}`,
		"repo vide":           `{"default":"a","profiles":{"a":{"repo":""}}}`,
		"repo absent":         `{"default":"a","profiles":{"a":{}}}`,
		"repo contrôle":       `{"default":"a","profiles":{"a":{"repo":"x\ny"}}}`,
		"repo non texte":      `{"default":"a","profiles":{"a":{"repo":42}}}`,
		"default inconnu":     `{"default":"b","profiles":{"a":{"repo":"r"}}}`,
		"default absent":      `{"profiles":{"a":{"repo":"r"}}}`,
		"default sans profil": `{"default":"a","profiles":{}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, content)); err == nil {
				t.Fatal("registre invalide accepté")
			}
		})
	}
}

func TestLoadAcceptsEmptyRegistry(t *testing.T) {
	for _, content := range []string{`{}`, `{"profiles":{}}`, `{"default":"","profiles":{}}`} {
		if _, err := Load(write(t, content)); err != nil {
			t.Fatalf("%s : %v", content, err)
		}
	}
}

func TestAddFirstProfileBecomesDefault(t *testing.T) {
	r := &Registry{}
	if err := r.Add("perso", "https://example.com/p.git"); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("acmecorp", "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if r.Default != "perso" || !reflect.DeepEqual(r.Keys(), []string{"perso", "acmecorp"}) {
		t.Fatalf("état inattendu : %+v", r)
	}
	if err := r.Add("perso", "x"); err == nil {
		t.Fatal("doublon accepté")
	}
	if err := r.Add("bad key", "x"); err == nil {
		t.Fatal("clé invalide acceptée")
	}
	if err := r.Add("ok", ""); err == nil {
		t.Fatal("repo vide accepté")
	}
}

func TestRemove(t *testing.T) {
	r := &Registry{}
	_ = r.Add("a", "ra")
	_ = r.Add("b", "rb")
	if err := r.Remove("a"); err == nil || !strings.Contains(err.Error(), "par défaut") {
		t.Fatalf("défaut retiré malgré d'autres profils : %v", err)
	}
	if err := r.Remove("zzz"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("err = %v", err)
	}
	if err := r.Remove("b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if r.Default != "" || len(r.Profiles) != 0 {
		t.Fatalf("registre non vidé : %+v", r)
	}
}

func TestGetSetUnset(t *testing.T) {
	r := &Registry{}
	_ = r.Add("perso", "https://example.com/p.git")
	if v, err := r.Get("default"); err != nil || v != "perso" {
		t.Fatalf("default = %q, %v", v, err)
	}
	if v, err := r.Get("profiles.perso.repo"); err != nil || v != "https://example.com/p.git" {
		t.Fatalf("repo = %q, %v", v, err)
	}
	if _, err := r.Get("profiles.nope.repo"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("err = %v", err)
	}
	for _, k := range []string{"", "defaults", "profiles", "profiles..repo", "profiles.perso", "profiles.perso.name"} {
		if _, err := r.Get(k); !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("Get(%q) err = %v", k, err)
		}
	}
	if err := r.Set("profiles.perso.repo", "https://example.com/new.git"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("profiles.perso.repo", ""); err == nil {
		t.Fatal("repo vide accepté")
	}
	if err := r.Set("profiles.acme.repo", "https://example.com/acme.git"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("default", "acme"); err != nil || r.Default != "acme" {
		t.Fatalf("Default = %q, %v", r.Default, err)
	}
	if err := r.Set("default", "ghost"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("err = %v", err)
	}
	if err := r.Set("profiles.a.b.repo", "x"); err != nil {
		t.Fatalf("clé pointée : %v", err)
	}
	if v, _ := r.Get("profiles.a.b.repo"); v != "x" {
		t.Fatalf("repo = %q", v)
	}
	if err := r.Unset("default"); err == nil {
		t.Fatal("default retiré malgré des profils")
	}
	if err := r.Unset("profiles.acme.repo"); err == nil {
		t.Fatal("profil par défaut retiré")
	}
	if err := r.Unset("profiles.a.b.repo"); err != nil {
		t.Fatal(err)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRoundTripAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".dot")
	path := filepath.Join(dir, "profiles.json")
	r := &Registry{}
	_ = r.Add("perso", "https://example.com/a?x=1&y=2")
	_ = r.Add("acmecorp", "https://example.com/b.git")
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode fichier = %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode dossier = %v", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "x=1&y=2") {
		t.Fatalf("& échappé : %s", data)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, r) {
		t.Fatalf("aller-retour : %+v != %+v", back, r)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("fichier temporaire résiduel : %v", entries)
	}
}

func TestSaveEmptyRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := (&Registry{}).Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRefusesInvalidAndKeepsOldFile(t *testing.T) {
	path := write(t, `{"default":"a","profiles":{"a":{"repo":"r"}}}`)
	before, _ := os.ReadFile(path)
	if err := (&Registry{Default: "ghost"}).Save(path); err == nil {
		t.Fatal("registre invalide écrit")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("fichier modifié par un échec")
	}
}

func TestSaveFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Registry{}
	_ = r.Add("a", "r")
	if err := r.Save(path); err == nil {
		t.Fatal("rename sur un dossier non vide accepté")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "profiles.json" {
		t.Fatalf("résidus : %v", entries)
	}
}
