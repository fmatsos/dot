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
		t.Fatalf("registry not empty: %+v", r)
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
		"unknown root":        `{"profiles":{},"extra":1}`,
		"profile field":       `{"default":"a","profiles":{"a":{"repo":"r","extra":1}}}`,
		"profiles not object": `{"profiles":[]}`,
		"profiles null":       `{"profiles":null}`,
		"invalid json":        `{`,
		"empty":               ``,
		"two documents":       `{"profiles":{}} {"profiles":{}}`,
		"array":               `[]`,
		"duplicate key":       `{"default":"a","profiles":{"a":{"repo":"r"},"a":{"repo":"s"}}}`,
		"invalid key":         `{"default":"a/b","profiles":{"a/b":{"repo":"r"}}}`,
		"key starts with .":   `{"default":".a","profiles":{".a":{"repo":"r"}}}`,
		"key with ..":         `{"default":"a..b","profiles":{"a..b":{"repo":"r"}}}`,
		"empty repo":          `{"default":"a","profiles":{"a":{"repo":""}}}`,
		"missing repo":        `{"default":"a","profiles":{"a":{}}}`,
		"repo with control":   `{"default":"a","profiles":{"a":{"repo":"x\ny"}}}`,
		"repo not text":       `{"default":"a","profiles":{"a":{"repo":42}}}`,
		"unknown default":     `{"default":"b","profiles":{"a":{"repo":"r"}}}`,
		"missing default":     `{"profiles":{"a":{"repo":"r"}}}`,
		"default no profile":  `{"default":"a","profiles":{}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, content)); err == nil {
				t.Fatal("invalid registry accepted")
			}
		})
	}
}

func TestLoadAcceptsEmptyRegistry(t *testing.T) {
	for _, content := range []string{`{}`, `{"profiles":{}}`, `{"default":"","profiles":{}}`} {
		if _, err := Load(write(t, content)); err != nil {
			t.Fatalf("%s: %v", content, err)
		}
	}
}

func TestAddFirstProfileBecomesDefault(t *testing.T) {
	r := &Registry{}
	if err := r.Add("personal", "https://example.com/p.git"); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("acmecorp", "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if r.Default != "personal" || !reflect.DeepEqual(r.Keys(), []string{"personal", "acmecorp"}) {
		t.Fatalf("unexpected state: %+v", r)
	}
	if err := r.Add("personal", "x"); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := r.Add("bad key", "x"); err == nil {
		t.Fatal("invalid key accepted")
	}
	if err := r.Add("ok", ""); err == nil {
		t.Fatal("empty repo accepted")
	}
}

func TestRemove(t *testing.T) {
	r := &Registry{}
	_ = r.Add("a", "ra")
	_ = r.Add("b", "rb")
	if err := r.Remove("a"); err == nil || errors.Is(err, ErrUnknownProfile) || len(r.Profiles) != 2 || r.Default != "a" {
		t.Fatalf("default removed despite other profiles: %v, %+v", err, r)
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
		t.Fatalf("registry not emptied: %+v", r)
	}
}

func TestGetSetUnset(t *testing.T) {
	r := &Registry{}
	_ = r.Add("personal", "https://example.com/p.git")
	if v, err := r.Get("default"); err != nil || v != "personal" {
		t.Fatalf("default = %q, %v", v, err)
	}
	if v, err := r.Get("profiles.personal.repo"); err != nil || v != "https://example.com/p.git" {
		t.Fatalf("repo = %q, %v", v, err)
	}
	if _, err := r.Get("profiles.nope.repo"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("err = %v", err)
	}
	for _, k := range []string{"", "defaults", "profiles", "profiles..repo", "profiles.personal", "profiles.personal.name"} {
		if _, err := r.Get(k); !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("Get(%q) err = %v", k, err)
		}
	}
	if err := r.Set("profiles.personal.repo", "https://example.com/new.git"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("profiles.personal.repo", ""); err == nil {
		t.Fatal("empty repo accepted")
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
		t.Fatalf("dotted key: %v", err)
	}
	if v, _ := r.Get("profiles.a.b.repo"); v != "x" {
		t.Fatalf("repo = %q", v)
	}
	if err := r.Unset("default"); err == nil {
		t.Fatal("default removed despite profiles")
	}
	if err := r.Unset("profiles.acme.repo"); err == nil {
		t.Fatal("default profile removed")
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
	_ = r.Add("personal", "https://example.com/a?x=1&y=2")
	_ = r.Add("acmecorp", "https://example.com/b.git")
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "x=1&y=2") {
		t.Fatalf("& escaped: %s", data)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, r) {
		t.Fatalf("round trip: %+v != %+v", back, r)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("leftover temporary file: %v", entries)
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
		t.Fatal("invalid registry written")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("file modified by a failure")
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
		t.Fatal("rename onto a non-empty directory accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "profiles.json" {
		t.Fatalf("leftovers: %v", entries)
	}
}
