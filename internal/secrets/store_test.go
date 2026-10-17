package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
  f="$FAKE_STORE/bw-$3"; [ "$2" = password ] || f="$FAKE_STORE/bw-$2-$3"
  cat "$f" 2>/dev/null || exit 1 ;;
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
'item view --share-id '*' --item-id '*) cat "$FAKE_STORE/pass-$6" 2>/dev/null || exit 1 ;;
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

// A Bitwarden reference reaches every field: a login's own ones through bw get, the others in the
// item's JSON, where a custom field called like a login field needs the fields. prefix.
func TestReadBitwardenFields(t *testing.T) {
	f := newFake(t)
	f.put(t, "bw-username-Login", "fake-user\n")
	f.put(t, "bw-totp-Login", "123456\n")
	f.put(t, "bw-item-Multi", `{"fields":[{"name":"Api_Key","value":"fake-hidden","type":1},{"name":"username","value":"fake-custom","type":0},{"name":"flag","value":true,"type":2},{"name":"linked","value":null,"type":3}],
		"card":{"number":"4242","expMonth":"04","code":"123"},
		"identity":{"email":"fake@example.test","passportNumber":"P0"},
		"sshKey":{"privateKey":"-----BEGIN FAKE-----\nabc\n-----END FAKE-----\n","publicKey":"ssh-ed25519 FAKE"}}`)
	ctx := context.Background()
	for _, tc := range []struct{ ref, want string }{
		{"bw:Login#username", "fake-user"},
		{"bw:Login#TOTP", "123456"},
		{"bw:Multi#api_key", "fake-hidden"},
		{"bw:Multi#fields.username", "fake-custom"},
		{"bw:Multi#flag", "true"},
		{"bw:Multi#card.number", "4242"},
		{"bw:Multi#Card.expMonth", "04"},
		{"bw:Multi#identity.email", "fake@example.test"},
		{"bw:Multi#identity.passportnumber", "P0"},
		{"bw:Multi#ssh.privateKey", "-----BEGIN FAKE-----\nabc\n-----END FAKE-----\n"},
		{"bw:Multi#sshkey.publicKey", "ssh-ed25519 FAKE"},
	} {
		if got, err := f.Read(ctx, Mapping{"ACME_TOKEN", tc.ref}); err != nil || got != tc.want {
			t.Errorf("Read(%s) = %q, %v", tc.ref, got, err)
		}
	}
	for _, ref := range []string{"bw:Multi#nothing", "bw:Multi#linked", "bw:Multi#card.nothing", "bw:Missing#card.number", "bw:Multi#username"} {
		if got, err := f.Read(ctx, Mapping{"ACME_TOKEN", ref}); err == nil || errors.Is(err, ErrLocked) || got != "" {
			t.Errorf("Read(%s) = %q, %v", ref, got, err)
		}
	}
	if !strings.Contains(f.argv(t), "bw get username Login\n") || !strings.Contains(f.argv(t), "bw get item Multi\n") {
		t.Errorf("argv:\n%s", f.argv(t))
	}
}

// A Share ID or an Item ID in a pass reference is looked up by id, whatever the other part is.
func TestReadPassByID(t *testing.T) {
	f := newFake(t)
	share, item := strings.Repeat("S", 86)+"==", strings.Repeat("i", 40)+strings.Repeat("-", 46)+"=="
	f.put(t, "pass-"+item, "fake-by-id\n")
	f.put(t, "live", "")
	ctx := context.Background()
	if got, err := f.Read(ctx, passMap(share+"/"+item)); err != nil || got != "fake-by-id" {
		t.Errorf("Read by ids = %q, %v", got, err)
	}
	if got, err := f.Read(ctx, passMap("Vault/"+item)); err != nil || got != "fake-by-id" {
		t.Errorf("Read by name and id = %q, %v", got, err)
	}
	if _, err := f.Add(ctx, "ACME_ID", "pass:Vault/"+strings.Repeat("x", 86)+"==", func() (string, error) { t.Fatal("input called"); return "", nil }); err == nil {
		t.Error("an unknown item id was created as a title")
	}
	a := f.argv(t)
	for _, want := range []string{"item view --share-id " + share + " --item-id " + item + " --field password\n", "item view --vault-name Vault --item-id " + item + " --field password\n"} {
		if !strings.Contains(a, want) {
			t.Errorf("argv lacks %q:\n%s", want, a)
		}
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
	want := []Entry{{"A", "bw", Locked}, {"B", "pass", Readable}, {"C", "pass", Unavailable}}
	if !slices.Equal(es, want) {
		t.Errorf("status = %v", es)
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
	if _, err := Status(context.Background(), dir); err == nil || errors.Is(err, ErrNoFile) {
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
	if _, err := f.Resolve(context.Background(), []string{"NOPE"}); err == nil || !strings.Contains(err.Error(), "NOPE") {
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

func TestAddRegistersExistingBitwardenField(t *testing.T) {
	f := newFake(t)
	f.put(t, "bw-item-Multi", `{"fields":[{"name":"api_key","value":"fake","type":1}]}`)
	ctx := context.Background()
	none := func() (string, error) { t.Fatal("input called"); return "", nil }
	if created, err := f.Add(ctx, "ACME_KEY", "bw:Multi#api_key", none); err != nil || created {
		t.Fatalf("Add = %v, %v", created, err)
	}
	if got := f.mapping(t); got != "ACME_KEY=\"bw:Multi#api_key\"\n" {
		t.Errorf("mapping: %q", got)
	}
	if _, err := f.Add(ctx, "ACME_NEW", "bw:Multi#missing", none); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Add of a missing field = %v", err)
	}
	if strings.Contains(f.mapping(t), "ACME_NEW") {
		t.Error("a missing field was registered")
	}
}

func TestAddRefusals(t *testing.T) {
	never := func() (string, error) { t.Error("input asked"); return "", nil }
	for _, tc := range []struct {
		env  string
		name string
		ref  string
		want error // the sentinel the refusal is, when it has one
	}{
		{"FAKE_LIST=[{\"name\":\"ACME\"},{\"name\":\"ACME staging\"}]", "N", "bw:ACME", nil}, // ambiguous search
		{"FAKE_LIST=[{\"name\":\"ACME staging\"}]", "N", "bw:ACME", nil},                     // ambiguous search
		{"FAKE_LIST=not json", "N", "bw:ACME", nil},                                          // search fails
		{"FAKE_STATE=locked", "N", "bw:ACME", ErrLocked},
		{"FAKE_PASS_OUT=1", "N", "pass:Personal/X", ErrPassLoggedOut},
		{"X=1", "N", "pass:V/I#password", nil}, // a #field is read, never created
		{"X=1", "1BAD", "bw:ACME", ErrMalformed},
		{"X=1", "N", "bw:", ErrMalformed},
		{"X=1", "DUP", "bw:ACME", nil}, // NAME already registered
	} {
		f := newFake(t, tc.env)
		os.WriteFile(filepath.Join(f.dir, File), []byte("DUP=bw:Other\n"), 0o600)
		_, err := f.Add(context.Background(), tc.name, tc.ref, never)
		if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s %s : %v, want %v", tc.name, tc.ref, err, tc.want)
		}
		if f.mapping(t) != "DUP=bw:Other\n" {
			t.Errorf("mapping changed: %q", f.mapping(t))
		}
		// refused before anything was created: the causes that carry no sentinel differ only by wording
		if _, statErr := os.Stat(filepath.Join(f.store, "created.json")); statErr == nil {
			t.Errorf("%s %s : an item was created", tc.name, tc.ref)
		}
	}
}

func TestAddReadbackMismatchLeavesMappingUnchanged(t *testing.T) {
	for name, content := range map[string]string{"Mismatch": "different\n", "Empty": ""} {
		f := newFake(t)
		f.put(t, "bw-"+name, content)
		_, err := f.Add(context.Background(), "N", "bw:"+name, func() (string, error) { return "fake-new", nil })
		if err == nil {
			t.Errorf("%s: unverified item accepted", name)
		}
		if _, statErr := os.Stat(filepath.Join(f.store, "created.json")); statErr != nil { // created, then refused on the read back
			t.Errorf("%s: item not created", name)
		}
		if _, statErr := os.Stat(filepath.Join(f.dir, File)); statErr == nil {
			t.Errorf("%s: mapping file written", name)
		}
	}
	f := newFake(t)
	if _, err := f.Add(context.Background(), "N", "bw:Item", func() (string, error) { return "", nil }); err == nil {
		t.Error("empty input accepted")
	}
	if _, statErr := os.Stat(filepath.Join(f.store, "created.json")); statErr == nil {
		t.Error("an item was created from an empty value")
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
	before := f.argv(t)
	if err := f.Unlock(context.Background(), "wrong"); err == nil {
		t.Error("wrong password accepted")
	}
	if f.argv(t) == before { // bw was asked: the refusal comes from it
		t.Error("bw not called for a wrong password")
	}
	before = f.argv(t)
	if err := f.Unlock(context.Background(), ""); err == nil {
		t.Error("empty password accepted")
	}
	if f.argv(t) != before { // refused before any call to bw
		t.Errorf("bw called for an empty password:\n%s", f.argv(t))
	}
	if err := f.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("cache not removed")
	}
	f.Env = append(f.Env, "FAKE_LOCK_FAIL=1")
	os.WriteFile(cache, []byte("x"), 0o600)
	if err := f.Lock(context.Background()); err == nil {
		t.Error("failing lock not reported")
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("cache kept after a failing lock")
	}
}
