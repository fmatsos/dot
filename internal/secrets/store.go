package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Sentinel errors the command layer maps to exit code 3 (message printed as is).
var (
	ErrLocked        = errors.New(`Bitwarden locked: run "dot secrets unlock"`)
	ErrPassLoggedOut = errors.New("Proton Pass: not logged in (pass-cli login)")
)

// Store reads and manages the secrets of one profile directory.
type Store struct {
	Dir string
	Env []string // environment of the process (KEY=VALUE); nil means os.Environ()

	session string // BW_SESSION found unlocked by a previous check
}

// New returns a Store for a profile directory, using the current process environment.
func New(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) environ() []string {
	if s.Env != nil {
		return s.Env
	}
	return os.Environ()
}

func (s *Store) cachePath() string { return filepath.Join(s.Dir, "bw-session.local") }

// OverrideEnv returns base without the variables set in kv, followed by kv ("NAME=value").
func OverrideEnv(base, kv []string) []string {
	drop := make(map[string]bool, len(kv))
	for _, e := range kv {
		k, _, _ := strings.Cut(e, "=")
		drop[k] = true
	}
	out := make([]string, 0, len(base)+len(kv))
	for _, e := range base {
		if k, _, _ := strings.Cut(e, "="); !drop[k] {
			out = append(out, e)
		}
	}
	return append(out, kv...)
}

// run executes a manager CLI. Its stderr is discarded (it could echo a value) and its stdin is
// /dev/null unless given. Variables that only one call may see are removed from the inherited
// environment first; extra ("NAME=value") is that call's own.
func (s *Store) run(ctx context.Context, extra []string, stdin io.Reader, name string, args ...string) ([]byte, error) {
	var env []string
	for _, e := range s.environ() {
		if k, _, _ := strings.Cut(e, "="); k != "DOT_SECRET_VALUE" && k != "BW_PASSWORD" {
			env = append(env, e)
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = OverrideEnv(env, extra)
	cmd.Stdin = stdin
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.Bytes(), err
}

// unlockedSession returns a BW_SESSION (environment, else the cache file) that bw reports unlocked.
func (s *Store) unlockedSession(ctx context.Context) (string, error) {
	if s.session != "" {
		return s.session, nil
	}
	sess := ""
	for _, e := range s.environ() {
		if v, ok := strings.CutPrefix(e, "BW_SESSION="); ok {
			sess = v
		}
	}
	if sess == "" {
		if b, err := os.ReadFile(s.cachePath()); err == nil {
			sess = strings.TrimRight(string(b), "\n")
		}
	}
	if sess == "" {
		return "", ErrLocked
	}
	out, err := s.run(ctx, []string{"BW_SESSION=" + sess}, nil, "bw", "status")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrLocked
	}
	var st struct{ Status string }
	if json.Unmarshal(out, &st) != nil || st.Status != "unlocked" {
		return "", ErrLocked
	}
	s.session = sess
	return sess, nil
}

// Read returns the value of one mapping. A locked Bitwarden gives ErrLocked; any other failure
// gives an error whose text never contains a value. One final newline of the manager is removed.
func (s *Store) Read(ctx context.Context, m Mapping) (string, error) {
	var out []byte
	var err error
	switch m.Backend() {
	case "bw":
		sess, serr := s.unlockedSession(ctx)
		if serr != nil {
			return "", serr
		}
		out, err = s.readBW(ctx, sess, m)
	case "pass":
		out, err = s.run(ctx, nil, nil, "pass-cli", m.PassView()...)
	default:
		err = errors.New("unknown backend")
	}
	if err != nil {
		return "", fmt.Errorf("%s (%s): read failed", m.Name, m.Backend())
	}
	v := strings.TrimSuffix(string(out), "\n")
	if v == "" {
		return "", fmt.Errorf("%s (%s): empty value", m.Name, m.Backend())
	}
	return v, nil
}

// readBW returns what bw prints for the field of m: a login's own field through bw get, any
// other one (card.*, identity.*, ssh.*, fields.NAME or a custom field NAME) from the item's JSON,
// which stays in memory. The result ends with a newline, as bw's own output would.
func (s *Store) readBW(ctx context.Context, sess string, m Mapping) ([]byte, error) {
	extra := []string{"BW_SESSION=" + sess}
	field := m.Field()
	switch f := strings.ToLower(field); f {
	case "password", "username", "uri", "totp", "notes":
		return s.run(ctx, extra, nil, "bw", "get", f, m.BwItem())
	}
	out, err := s.run(ctx, extra, nil, "bw", "get", "item", m.BwItem())
	if err != nil {
		return nil, err
	}
	v, ok := bwField(out, field)
	if !ok {
		return nil, errors.New("field not found")
	}
	return []byte(v + "\n"), nil
}

// bwField picks one field out of the JSON of a Bitwarden item: "card.number", "identity.email",
// "ssh.privateKey" (also "sshkey."), "fields.NAME" or, with no prefix, a custom field by name.
// Property and field names match exactly first, then without regard to case.
func bwField(data []byte, field string) (string, bool) {
	var it struct {
		Fields []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"fields"`
		Card     map[string]any `json:"card"`
		Identity map[string]any `json:"identity"`
		SSHKey   map[string]any `json:"sshKey"`
	}
	if json.Unmarshal(data, &it) != nil {
		return "", false
	}
	props := []struct {
		prefix string
		m      map[string]any
	}{{"card.", it.Card}, {"identity.", it.Identity}, {"ssh.", it.SSHKey}, {"sshkey.", it.SSHKey}}
	for _, p := range props {
		if name, ok := cutPrefixFold(field, p.prefix); ok {
			for _, same := range matchers(name) {
				for key, v := range p.m {
					if same(key) {
						return scalar(v)
					}
				}
			}
			return "", false
		}
	}
	name, _ := cutPrefixFold(field, "fields.")
	if name == "" {
		name = field
	}
	for _, same := range matchers(name) {
		for _, f := range it.Fields {
			if same(f.Name) {
				return scalar(f.Value)
			}
		}
	}
	return "", false
}

// matchers tries a name as is, then without regard to case.
func matchers(name string) []func(string) bool {
	return []func(string) bool{func(s string) bool { return s == name }, func(s string) bool { return strings.EqualFold(s, name) }}
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) > len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}

// scalar renders a JSON string, number or boolean; null and structures are not a value.
func scalar(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case bool, float64:
		return fmt.Sprint(v), true
	}
	return "", false
}

// Get returns the value of the secret called name in secrets.local.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	ms, err := Load(s.Dir)
	if err != nil {
		return "", err
	}
	m, err := Lookup(ms, name)
	if err != nil {
		return "", err
	}
	return s.Read(ctx, m)
}

// Resolve resolves every named secret first and returns them as "NAME=value" entries.
func (s *Store) Resolve(ctx context.Context, names []string) ([]string, error) {
	var kv []string
	for _, n := range names {
		v, err := s.Get(ctx, n)
		if err != nil {
			return nil, err
		}
		kv = append(kv, n+"="+v)
	}
	return kv, nil
}

// State is the readability of one secret.
type State string

const (
	Readable    State = "readable"
	Locked      State = "locked"
	Unavailable State = "unavailable"
)

// Entry is the status of one mapping. It never holds a value.
type Entry struct {
	Name    string
	Backend string
	State   State
}

func (e Entry) String() string { return fmt.Sprintf("%s (%s): %s", e.Name, e.Backend, e.State) }

// Status reports every secret of dir/secrets.local as readable, locked or unavailable, reading
// each value only to test it. An error means the mapping file itself is missing or malformed.
// Once ctx expires, the remaining secrets are reported unavailable: a caller such as `dot doctor`
// bounds the whole call with context.WithTimeout(ctx, 20*time.Second).
func Status(ctx context.Context, dir string) ([]Entry, error) { return New(dir).Status(ctx) }

// Status is the method form of the package function, for a Store with its own environment.
func (s *Store) Status(ctx context.Context) ([]Entry, error) {
	ms, err := Load(s.Dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(ms))
	for _, m := range ms {
		st := Readable
		if _, err := s.Read(ctx, m); err != nil {
			st = Unavailable
			if errors.Is(err, ErrLocked) {
				st = Locked
			}
		}
		entries = append(entries, Entry{m.Name, m.Backend(), st})
	}
	return entries, nil
}

// writeAtomic replaces dir/name with data through a private temporary file (0600).
func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+name+".")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// Unlock unlocks Bitwarden with the master password, given to bw through its environment, and
// keeps the session in bw-session.local (0600).
func (s *Store) Unlock(ctx context.Context, password string) error {
	if password == "" {
		return errors.New("Bitwarden: empty password")
	}
	out, err := s.run(ctx, []string{"BW_PASSWORD=" + password}, nil, "bw", "unlock", "--passwordenv", "BW_PASSWORD", "--raw")
	if err != nil {
		return errors.New("Bitwarden: unlock failed")
	}
	sess := strings.TrimRight(string(out), "\n")
	if sess == "" {
		return errors.New("Bitwarden: empty session")
	}
	s.session = ""
	return writeAtomic(s.Dir, "bw-session.local", []byte(sess))
}

// Lock locks Bitwarden and removes the kept session, even when bw fails.
func (s *Store) Lock(ctx context.Context) error {
	_, err := s.run(ctx, nil, nil, "bw", "lock")
	os.Remove(s.cachePath())
	s.session = ""
	if err != nil {
		return errors.New("Bitwarden: lock failed")
	}
	return nil
}
