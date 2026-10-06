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
	for _, tc := range []struct{ in, want string }{
		{"OK=bw:x\nnot a mapping\n", "ligne 2 mal formée"},
		{"BAD=bw:\n", "ligne 1 mal formée"},
		{"\n\nBAD=pass:/Title\n", "ligne 3 mal formée"},
		{"BAD=other:Title\n", "ligne 1 mal formée"},
		{"A=\n", "ligne 1 mal formée"},
		{"A=bw:x\r\n", "ligne 1 mal formée"},
		{"A=bw:x\nB=bw:y\nA=bw:z\n", "ligne 3 : NAME en double"},
	} {
		_, err := Parse([]byte(tc.in))
		if err == nil || err.Error() != tc.want {
			t.Errorf("Parse(%q) = %v, want %q", tc.in, err, tc.want)
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
	if _, err := Lookup(ms, "UNKNOWN"); err == nil || err.Error() != "UNKNOWN : NAME inconnu" {
		t.Errorf("Lookup UNKNOWN = %v", err)
	}
}

func TestOverrideEnv(t *testing.T) {
	got := OverrideEnv([]string{"A=1", "B=2", "A=3", "C=4"}, []string{"A=x", "D=y"})
	if strings.Join(got, ",") != "B=2,C=4,A=x,D=y" {
		t.Errorf("got %v", got)
	}
}
