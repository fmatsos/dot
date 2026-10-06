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
	ErrLocked        = errors.New("Bitwarden verrouillé : lance « dot secrets unlock »")
	ErrPassLoggedOut = errors.New("Proton Pass : non connecté (pass-cli login)")
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
		out, err = s.run(ctx, []string{"BW_SESSION=" + sess}, nil, "bw", "get", "password", m.Item())
	case "pass":
		out, err = s.run(ctx, nil, nil, "pass-cli", "item", "view", "--vault-name", m.Vault(), "--item-title", m.Title(), "--field", "password")
	default:
		err = errors.New("backend inconnu")
	}
	if err != nil {
		return "", fmt.Errorf("%s (%s) : lecture impossible", m.Name, m.Backend())
	}
	v := strings.TrimSuffix(string(out), "\n")
	if v == "" {
		return "", fmt.Errorf("%s (%s) : valeur vide", m.Name, m.Backend())
	}
	return v, nil
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
	Readable    State = "lisible"
	Locked      State = "verrouillé"
	Unavailable State = "indisponible"
)

// Entry is the status of one mapping. It never holds a value.
type Entry struct {
	Name    string
	Backend string
	State   State
}

func (e Entry) String() string { return fmt.Sprintf("%s (%s) : %s", e.Name, e.Backend, e.State) }

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
		return errors.New("Bitwarden : mot de passe vide")
	}
	out, err := s.run(ctx, []string{"BW_PASSWORD=" + password}, nil, "bw", "unlock", "--passwordenv", "BW_PASSWORD", "--raw")
	if err != nil {
		return errors.New("Bitwarden : déverrouillage impossible")
	}
	sess := strings.TrimRight(string(out), "\n")
	if sess == "" {
		return errors.New("Bitwarden : session vide")
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
		return errors.New("Bitwarden : verrouillage impossible")
	}
	return nil
}
