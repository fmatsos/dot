package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fictional managers: they refuse DOT_SECRET_VALUE and BW_PASSWORD (except unlock's) in their
// environment, log their argv and keep what they receive in $FAKE_STORE.
const fakeBW = `#!/usr/bin/env bash
[ "${DOT_SECRET_VALUE+x}" != x ] || exit 99
echo "bw $*" >>"$FAKE_STORE/argv"
case $1 in
unlock) [ "${BW_PASSWORD:-}" = fake-master ] && [ "$3" = BW_PASSWORD ] || exit 1; printf 'fake-session\n' ;;
status) [ "${BW_SESSION:-}" = fake-session ] || exit 1; printf '{ "status": "%s" }' "${FAKE_STATE:-unlocked}" ;;
lock) [ "${FAKE_LOCK_FAIL:-}" = 1 ] && exit 1; exit 0 ;;
list) printf '%s' "${FAKE_LIST:-[]}" ;;
encode) cat ;;
create) cat >"$FAKE_STORE/created.json" ;;
get)
  if [ "$2" = template ]; then echo '{"type":null,"name":null,"notes":"n","login":null,"reprompt":0}'; exit; fi
  [ "${BW_PASSWORD+x}" != x ] || exit 98
  cat "$FAKE_STORE/bw-$3" 2>/dev/null || exit 1 ;;
*) exit 1 ;;
esac
`

const fakePass = `#!/usr/bin/env bash
[ "${DOT_SECRET_VALUE+x}" != x ] || exit 99
echo "pass-cli $*" >>"$FAKE_STORE/argv"
case "$*" in
info) [ "${FAKE_PASS_OUT:-}" != 1 ] ;;
'item create login --get-template') echo '{"title":"","password":"","urls":[]}' ;;
'item create login --vault-name '*' --from-template -') cat >"$FAKE_STORE/created.json" ;;
'item view --vault-name '*) [ -e "$FAKE_STORE/created.json" ] || [ -e "$FAKE_STORE/live" ] || exit 1; cat "$FAKE_STORE/pass-$6" 2>/dev/null || exit 1 ;;
*) exit 1 ;;
esac
`

type fake struct {
	*Store
	dir, store string
}

func newFake(t *testing.T, extra ...string) *fake {
	t.Helper()
	bin, store, dir := t.TempDir(), t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"bw": fakeBW, "pass-cli": fakePass} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	env := append(os.Environ(), "FAKE_STORE="+store, "BW_SESSION=fake-session")
	return &fake{&Store{Dir: dir, Env: append(env, extra...)}, dir, store}
}

func (f *fake) put(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.store, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fake) argv(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(f.store, "argv"))
	return string(b)
}

func (f *fake) mapping(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(f.dir, File))
	return string(b)
}

func bwMap(item string) Mapping   { return Mapping{"ACME_TOKEN", "bw:" + item} }
func passMap(item string) Mapping { return Mapping{"ACME_PASS", "pass:" + item} }

func TestReadStripsOneNewline(t *testing.T) {
	f := newFake(t)
	f.put(t, "bw-Newlines", "fake\n\n\n")
	f.put(t, "bw-Single", "fake-value\n")
	f.put(t, "pass-Item with spaces", "fake-pass\n")
	f.put(t, "live", "")
	for _, tc := range []struct {
		m    Mapping
		want string
	}{
		{bwMap("Newlines"), "fake\n\n"},
		{bwMap("Single"), "fake-value"},
		{passMap("Vault/Item with spaces"), "fake-pass"},
	} {
		if got, err := f.Read(context.Background(), tc.m); err != nil || got != tc.want {
			t.Errorf("Read(%s) = %q, %v", tc.m.Ref, got, err)
		}
	}
	if !strings.Contains(f.argv(t), "pass-cli item view --vault-name Vault --item-title Item with spaces --field password\n") {
		t.Errorf("argv:\n%s", f.argv(t))
	}
}

func TestReadFailuresNeverEchoValues(t *testing.T) {
	f := newFake(t)
	f.put(t, "bw-Empty", "")
	for _, m := range []Mapping{bwMap("Empty"), bwMap("Missing"), passMap("Vault/Missing")} {
		_, err := f.Read(context.Background(), m)
		if err == nil || errors.Is(err, ErrLocked) {
			t.Errorf("Read(%s) = %v", m.Ref, err)
		}
	}
	_, err := f.Read(context.Background(), bwMap("Empty"))
	if err == nil || err.Error() != "ACME_TOKEN (bw) : valeur vide" {
		t.Errorf("empty = %v", err)
	}
}

func TestLockedStates(t *testing.T) {
	for _, state := range []string{"locked", "unauthenticated"} {
		f := newFake(t, "FAKE_STATE="+state)
		if _, err := f.Read(context.Background(), bwMap("X")); !errors.Is(err, ErrLocked) {
			t.Errorf("%s: %v", state, err)
		}
	}
	// No BW_SESSION and no cache file: locked without calling bw at all.
	f := newFake(t, "BW_SESSION=")
	if _, err := f.Read(context.Background(), bwMap("X")); !errors.Is(err, ErrLocked) || strings.Contains(f.argv(t), "bw status") {
		t.Errorf("no session: %v / %s", err, f.argv(t))
	}
	// The cache file supplies the session.
	f.put(t, "bw-X", "fake-value")
	if err := os.WriteFile(filepath.Join(f.dir, "bw-session.local"), []byte("fake-session\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Read(context.Background(), bwMap("X")); err != nil || got != "fake-value" {
		t.Errorf("cache session: %q, %v", got, err)
	}
}

func TestStatusAndTimeout(t *testing.T) {
	f := newFake(t, "FAKE_STATE=locked")
	f.put(t, "pass-Item", "fake-pass")
	f.put(t, "live", "")
	data := "A=bw:X\nB=pass:Vault/Item\nC=pass:Vault/Missing\n"
	if err := os.WriteFile(filepath.Join(f.dir, File), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	es, err := f.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range es {
		got = append(got, e.String())
	}
	want := "A (bw) : verrouillé,B (pass) : lisible,C (pass) : indisponible"
	if strings.Join(got, ",") != want {
		t.Errorf("status = %v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	es, err = f.Status(ctx)
	if err != nil || len(es) != 3 {
		t.Fatalf("%v %v", es, err)
	}
	for _, e := range es {
		if e.State != Unavailable {
			t.Errorf("expired context: %s", e)
		}
	}
}

func TestStatusFileErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Status(context.Background(), dir); !errors.Is(err, ErrNoFile) {
		t.Errorf("absent: %v", err)
	}
	os.WriteFile(filepath.Join(dir, File), []byte("garbage\n"), 0o600)
	if _, err := Status(context.Background(), dir); err == nil || err.Error() != "ligne 1 mal formée" {
		t.Errorf("malformed: %v", err)
	}
}

func TestResolveAllOrNothing(t *testing.T) {
	f := newFake(t)
	f.put(t, "bw-A", "va\n")
	f.put(t, "pass-B", "vb\n")
	f.put(t, "live", "")
	os.WriteFile(filepath.Join(f.dir, File), []byte("A_VAR=bw:A\nB_VAR=pass:Vault/B\nC_VAR=bw:Absent\n"), 0o600)
	kv, err := f.Resolve(context.Background(), []string{"A_VAR", "B_VAR"})
	if err != nil || strings.Join(kv, ",") != "A_VAR=va,B_VAR=vb" {
		t.Errorf("Resolve = %v, %v", kv, err)
	}
	if kv, err := f.Resolve(context.Background(), []string{"A_VAR", "C_VAR"}); err == nil || kv != nil {
		t.Errorf("failing secret: %v, %v", kv, err)
	}
	if _, err := f.Resolve(context.Background(), []string{"NOPE"}); err == nil || err.Error() != "NOPE : NAME inconnu" {
		t.Errorf("unknown: %v", err)
	}
}

func TestAddCreatesBitwardenItem(t *testing.T) {
	// DOT_SECRET_VALUE exported by the caller must never reach bw.
	f := newFake(t, "DOT_SECRET_VALUE=leak")
	f.put(t, "bw-Created", "fake-new\n")
	created, err := f.Add(context.Background(), "ACME_TOKEN", "bw:Created", func() (string, error) { return "fake-new", nil })
	if err != nil || !created {
		t.Fatalf("Add = %v, %v", created, err)
	}
	if f.mapping(t) != "ACME_TOKEN=bw:Created\n" {
		t.Errorf("mapping = %q", f.mapping(t))
	}
	if fi, _ := os.Stat(filepath.Join(f.dir, File)); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(filepath.Join(f.store, "created.json"))
	for _, want := range []string{`"type":1`, `"name":"Created"`, `"notes":null`, `"password":"fake-new"`, `"reprompt":0`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("created JSON lacks %s: %s", want, b)
		}
	}
	if strings.Contains(f.argv(t), "fake-new") {
		t.Errorf("value in argv:\n%s", f.argv(t))
	}
}

func TestAddCreatesPassItemAndAppendsLine(t *testing.T) {
	f := newFake(t)
	f.put(t, "pass-Created", "fake-new\n")
	os.WriteFile(filepath.Join(f.dir, File), []byte("OLD=bw:Old"), 0o600) // no final newline
	if _, err := f.Add(context.Background(), "ACME_PASS", "pass:Personal/Created", func() (string, error) { return "fake-new", nil }); err != nil {
		t.Fatal(err)
	}
	if f.mapping(t) != "OLD=bw:Old\nACME_PASS=pass:Personal/Created\n" {
		t.Errorf("mapping = %q", f.mapping(t))
	}
	b, _ := os.ReadFile(filepath.Join(f.store, "created.json"))
	if !strings.Contains(string(b), `"title":"Created"`) || !strings.Contains(string(b), `"password":"fake-new"`) || !strings.Contains(string(b), `"urls":[]`) {
		t.Errorf("created JSON: %s", b)
	}
}

func TestAddReusesExistingWithoutInput(t *testing.T) {
	asked := func() (string, error) { t.Error("input asked"); return "", nil }
	f := newFake(t, `FAKE_LIST=[{"name":"Existing"}]`)
	if created, err := f.Add(context.Background(), "A", "bw:Existing", asked); err != nil || created {
		t.Errorf("bw: %v, %v", created, err)
	}
	f.put(t, "pass-Existing", "x")
	f.put(t, "live", "")
	if created, err := f.Add(context.Background(), "B", "pass:Personal/Existing", asked); err != nil || created {
		t.Errorf("pass: %v, %v", created, err)
	}
	if f.mapping(t) != "A=bw:Existing\nB=pass:Personal/Existing\n" {
		t.Errorf("mapping = %q", f.mapping(t))
	}
}

func TestAddRefusals(t *testing.T) {
	never := func() (string, error) { t.Error("input asked"); return "", nil }
	for _, tc := range []struct {
		env  string
		name string
		ref  string
		want string
	}{
		{"FAKE_LIST=[{\"name\":\"ACME\"},{\"name\":\"ACME staging\"}]", "N", "bw:ACME", "Bitwarden : recherche ambiguë pour bw get, choisis un nom plus précis"},
		{"FAKE_LIST=[{\"name\":\"ACME staging\"}]", "N", "bw:ACME", "Bitwarden : recherche ambiguë pour bw get, choisis un nom plus précis"},
		{"FAKE_LIST=not json", "N", "bw:ACME", "Bitwarden : recherche impossible"},
		{"FAKE_STATE=locked", "N", "bw:ACME", ErrLocked.Error()},
		{"FAKE_PASS_OUT=1", "N", "pass:Personal/X", ErrPassLoggedOut.Error()},
		{"X=1", "1BAD", "bw:ACME", ErrMalformed.Error()},
		{"X=1", "N", "bw:", ErrMalformed.Error()},
		{"X=1", "DUP", "bw:ACME", "DUP : NAME déjà enregistré"},
	} {
		f := newFake(t, tc.env)
		os.WriteFile(filepath.Join(f.dir, File), []byte("DUP=bw:Other\n"), 0o600)
		_, err := f.Add(context.Background(), tc.name, tc.ref, never)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s %s : %v, want %q", tc.name, tc.ref, err, tc.want)
		}
		if f.mapping(t) != "DUP=bw:Other\n" {
			t.Errorf("mapping changed: %q", f.mapping(t))
		}
	}
}

func TestAddReadbackMismatchLeavesMappingUnchanged(t *testing.T) {
	for name, content := range map[string]string{"Mismatch": "different\n", "Empty": ""} {
		f := newFake(t)
		f.put(t, "bw-"+name, content)
		_, err := f.Add(context.Background(), "N", "bw:"+name, func() (string, error) { return "fake-new", nil })
		if err == nil || !strings.HasPrefix(err.Error(), "élément créé mais non enregistré") {
			t.Errorf("%s: %v", name, err)
		}
		if _, statErr := os.Stat(filepath.Join(f.dir, File)); statErr == nil {
			t.Errorf("%s: mapping file written", name)
		}
	}
	f := newFake(t)
	if _, err := f.Add(context.Background(), "N", "bw:Item", func() (string, error) { return "", nil }); err == nil || err.Error() != "valeur vide" {
		t.Errorf("empty input: %v", err)
	}
}

func TestUnlockAndLock(t *testing.T) {
	f := newFake(t, "BW_PASSWORD=inherited")
	cache := filepath.Join(f.dir, "bw-session.local")
	os.WriteFile(cache, []byte("old"), 0o644)
	if err := f.Unlock(context.Background(), "fake-master"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cache)
	fi, _ := os.Stat(cache)
	if string(b) != "fake-session" || fi.Mode().Perm() != 0o600 {
		t.Errorf("cache = %q, mode %v", b, fi.Mode().Perm())
	}
	if strings.Contains(f.argv(t), "fake-master") {
		t.Errorf("master password in argv:\n%s", f.argv(t))
	}
	if err := f.Unlock(context.Background(), "wrong"); err == nil || err.Error() != "Bitwarden : déverrouillage impossible" {
		t.Errorf("wrong password: %v", err)
	}
	if err := f.Unlock(context.Background(), ""); err == nil || err.Error() != "Bitwarden : mot de passe vide" {
		t.Errorf("empty password: %v", err)
	}
	if err := f.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("cache not removed")
	}
	f.Env = append(f.Env, "FAKE_LOCK_FAIL=1")
	os.WriteFile(cache, []byte("x"), 0o600)
	if err := f.Lock(context.Background()); err == nil || err.Error() != "Bitwarden : verrouillage impossible" {
		t.Errorf("failing lock: %v", err)
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("cache kept after a failing lock")
	}
}
