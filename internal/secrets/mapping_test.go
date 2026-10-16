package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	for _, tc := range []struct {
		name, ref string
		want      bool
	}{
		{"ACME_TOKEN", "bw:Chat login", true},
		{"_x1", "pass:Vault/Item with spaces", true},
		{"A", "pass:Vault/a/b", true},
		{"1BAD", "bw:x", false},
		{"", "bw:x", false},
		{"BAD-NAME", "bw:x", false},
		{"A", "bw:", false},
		{"A", "pass:/Title", false},
		{"A", "pass:Vault/", false},
		{"A", "pass:Vault/Item#Clé API", true},
		{"A", "pass:Vault/Item#", false},
		{"A", "pass:Vault/#f", false},
		{"A", "pass:NoSlash", false},
		{"A", "other:x", false},
		{"A", "bw:x\ny", false},
		{"A", "bw:x\r", false},
	} {
		if got := Valid(tc.name, tc.ref); got != tc.want {
			t.Errorf("Valid(%q, %q) = %v", tc.name, tc.ref, got)
		}
	}
}

func TestPassField(t *testing.T) {
	m := Mapping{"A", "pass:Vault/Item#Clé API"}
	if m.Title() != "Item" || m.Field() != "Clé API" {
		t.Errorf("title %q field %q", m.Title(), m.Field())
	}
	if m := (Mapping{"A", "pass:Vault/Item"}); m.Title() != "Item" || m.Field() != "password" {
		t.Errorf("title %q field %q", m.Title(), m.Field())
	}
}

func TestQuotedRefs(t *testing.T) {
	ms, err := Parse([]byte("A=\"pass:V/I#Clé API\"\nB='bw:Chat login'\nC=pass:V/I#Clé API\nD=\"bw:x"))
	if err == nil || !strings.Contains(err.Error(), "4") { // the number of the malformed line
		t.Fatalf("unbalanced quote must be malformed, got %v", err)
	}
	ms, err = Parse([]byte("A=\"pass:V/I#Clé API\"\nB='bw:Chat login'\nC=pass:V/I#Clé API\n"))
	if err != nil || len(ms) != 3 || ms[0].Ref != "pass:V/I#Clé API" || ms[1].Ref != "bw:Chat login" || ms[2].Ref != ms[0].Ref {
		t.Fatalf("%v %v", ms, err)
	}
	for _, tc := range []struct {
		m    Mapping
		want string
	}{
		{Mapping{"A", "pass:V/I#Clé API"}, `A="pass:V/I#Clé API"`},
		{Mapping{"A", "bw:Chat login"}, `A="bw:Chat login"`},
		{Mapping{"A", "bw:token"}, "A=bw:token"},
	} {
		if got := tc.m.Line(); got != tc.want {
			t.Errorf("Line() = %s", got)
		}
	}
}

func TestParse(t *testing.T) {
	ms, err := Parse([]byte("# comment\n\n  # indented\nACME_TOKEN=bw:Chat login\n  \nOTHER=pass:Vault/My item"))
	if err != nil || len(ms) != 2 {
		t.Fatalf("%v %v", ms, err)
	}
	if m := ms[1]; m.Name != "OTHER" || m.Backend() != "pass" || m.Vault() != "Vault" || m.Title() != "My item" || m.Item() != "Vault/My item" {
		t.Errorf("mapping = %+v", m)
	}
	if m := ms[0]; m.Backend() != "bw" || m.Item() != "Chat login" {
		t.Errorf("mapping = %+v", m)
	}
	if ms, err := Parse(nil); err != nil || len(ms) != 0 {
		t.Errorf("empty = %v %v", ms, err)
	}
}

func TestParseErrorsNameLineOnly(t *testing.T) {
	for _, tc := range []struct {
		in   string
		line string // the number of the refused line: the only data the error carries
	}{
		{"OK=bw:x\nnot a mapping\n", "2"},
		{"BAD=bw:\n", "1"},
		{"\n\nBAD=pass:/Title\n", "3"},
		{"BAD=other:Title\n", "1"},
		{"A=\n", "1"},
		{"A=bw:x\r\n", "1"},
		{"A=bw:x\nB=bw:y\nA=bw:z\n", "3"}, // a duplicate NAME
	} {
		_, err := Parse([]byte(tc.in))
		if err == nil || !strings.Contains(err.Error(), tc.line) {
			t.Errorf("Parse(%q) = %v, want line %s", tc.in, err, tc.line)
		}
	}
}

func TestLoadAndLookup(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); !errors.Is(err, ErrNoFile) {
		t.Fatalf("absent file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, File), []byte("A=bw:x\nB=pass:V/T\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m, err := Lookup(ms, "B"); err != nil || m.Ref != "pass:V/T" {
		t.Errorf("Lookup B = %+v, %v", m, err)
	}
	if _, err := Lookup(ms, "UNKNOWN"); err == nil || !strings.Contains(err.Error(), "UNKNOWN") {
		t.Errorf("Lookup UNKNOWN = %v", err)
	}
}

func TestOverrideEnv(t *testing.T) {
	got := OverrideEnv([]string{"A=1", "B=2", "A=3", "C=4"}, []string{"A=x", "D=y"})
	if strings.Join(got, ",") != "B=2,C=4,A=x,D=y" {
		t.Errorf("got %v", got)
	}
}
