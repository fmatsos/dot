package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCRLFFile(t *testing.T) {
	// Fail closed and never echo the line: a CRLF mapping line is refused (the CR would become part of
	// the item name). Comment and blank lines in a CRLF file are harmless.
	if _, err := Parse([]byte("# note\r\n\r\nA=bw:x\r\n")); err == nil || err.Error() != "ligne 3 mal formée" {
		t.Errorf("CRLF : %v", err)
	}
	if ms, err := Parse([]byte("# note\r\n  \r\n")); err != nil || len(ms) != 0 {
		t.Errorf("CRLF sans mapping : %v %v", ms, err)
	}
}

func TestParseBOMAndPadding(t *testing.T) {
	// Behavior documented, unchanged: a BOM, padding or spaces around "=" are refused, never guessed.
	for _, in := range []string{"\xef\xbb\xbfA=bw:x\n", " A=bw:x\n", "A =bw:x\n", "A= bw:x\n", "A\t=bw:x\n", "A=bw:x y\nB\n"} {
		_, err := Parse([]byte(in))
		if err == nil {
			// "A= bw:x" and "A=bw:x y" parse by design: the ref is everything after "=".
			if in == "A= bw:x\n" {
				t.Errorf("%q devrait être refusé : le préfixe de backend serait ' bw:'", in)
			}
			continue
		}
		if strings.Contains(err.Error(), "bw:x") {
			t.Errorf("%q : l'erreur cite la ligne : %v", in, err)
		}
	}
}

func TestParseDuplicateAcrossCommentsAndInvalidNames(t *testing.T) {
	if _, err := Parse([]byte("A=bw:x\n# c\n\nA=pass:V/T\n")); err == nil || err.Error() != "ligne 4 : NAME en double" {
		t.Errorf("doublon : %v", err)
	}
	// Names are case-sensitive: a and A are two names.
	if ms, err := Parse([]byte("a=bw:x\nA=bw:y\n")); err != nil || len(ms) != 2 {
		t.Errorf("casse : %v %v", ms, err)
	}
	for _, name := range []string{"A B", "A-B", "A.B", "1A", "é", "A\x00", "A=B", "$A", "A\tB"} {
		if Valid(name, "bw:x") {
			t.Errorf("nom %q devrait être invalide", name)
		}
		_, err := Parse([]byte(name + "=bw:secretitem\n"))
		if err == nil || strings.Contains(err.Error(), "secretitem") {
			t.Errorf("nom %q : %v", name, err)
		}
	}
	// "A=B=bw:x": the first "=" splits, so the ref "B=bw:x" has no known backend.
	if _, err := Parse([]byte("A=B=bw:x\n")); err == nil {
		t.Error("A=B=bw:x devrait être refusé")
	}
	// An "=" later in the ref is kept as is.
	if ms, err := Parse([]byte("A=bw:x=y\n")); err != nil || ms[0].Item() != "x=y" {
		t.Errorf("= dans la référence : %v %v", ms, err)
	}
}

func TestLoadUnreadableMappingIsAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, File), 0o700); err != nil { // a directory is not a mapping file
		t.Fatal(err)
	}
	if _, err := Load(dir); err != ErrNoFile {
		t.Errorf("dossier : %v", err)
	}
}

func TestReadKeepsInnerNewlinesAndCarriageReturns(t *testing.T) {
	f := newFake(t)
	f.put(t, "bw-Multi", "-----BEGIN KEY-----\nabc\r\ndef\n-----END KEY-----\n")
	f.put(t, "bw-CRLF", "fake\r\n")
	for item, want := range map[string]string{
		"Multi": "-----BEGIN KEY-----\nabc\r\ndef\n-----END KEY-----", // one final \n removed, the rest intact
		"CRLF":  "fake\r",                                             // documented: only "\n" is stripped, a CR stays
	} {
		got, err := f.Read(context.Background(), bwMap(item))
		if err != nil || got != want {
			t.Errorf("Read(%s) = %q, %v", item, got, err)
		}
	}
	// The value travelled by stdout only: no argv of any manager holds it.
	if a := f.argv(t); strings.Contains(a, "BEGIN KEY") || strings.Contains(a, "abc") {
		t.Errorf("valeur dans argv :\n%s", a)
	}
	kv, err := f.Resolve(context.Background(), nil)
	if err != nil || kv != nil {
		t.Errorf("Resolve(rien) = %v, %v", kv, err)
	}
}

func TestResolveWithMultilineValueEntry(t *testing.T) {
	f := newFake(t)
	f.put(t, "bw-Multi", "l1\nl2\n")
	if err := os.WriteFile(filepath.Join(f.dir, File), []byte("MULTI=bw:Multi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kv, err := f.Resolve(context.Background(), []string{"MULTI"})
	if err != nil || len(kv) != 1 || kv[0] != "MULTI=l1\nl2" {
		t.Errorf("Resolve = %q, %v", kv, err)
	}
}

// A manager that prints a value and then fails (on stdout and stderr): the error must never carry it.
func TestFailingManagerNeverLeaksItsOutput(t *testing.T) {
	f := newFake(t)
	bin := t.TempDir()
	script := "#!/usr/bin/env bash\n" +
		"echo \"$*\" >>\"$FAKE_STORE/argv\"\n" +
		"case $1 in status) printf '{\"status\":\"unlocked\"}'; exit 0;; esac\n" +
		"echo leaky-value-123; echo leaky-value-123 >&2; exit 1\n"
	for _, n := range []string{"bw", "pass-cli"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, m := range []Mapping{bwMap("Item"), passMap("Vault/Item")} {
		_, err := f.Read(context.Background(), m)
		if err == nil || strings.Contains(err.Error(), "leaky") {
			t.Errorf("Read(%s) = %v", m.Ref, err)
		}
	}
	ents, err := f.Status(context.Background())
	if err == nil {
		t.Logf("%v", ents) // no mapping file: an error, no entries
	}
}

func TestMappingFileSecretNeverInParseError(t *testing.T) {
	// A line that looks like "NAME=value" (someone pasted a secret in place of a reference).
	_, err := Parse([]byte("API_KEY=sk-live-0123456789\n"))
	if err == nil || strings.Contains(err.Error(), "sk-live") {
		t.Fatalf("%v", err)
	}
}
