// Package registry reads and writes ~/.dot/profiles.json, the machine-local list of profiles.
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	ErrUnknownKey     = errors.New("clé inconnue")
	ErrUnknownProfile = errors.New("profil inconnu")
	keyRe             = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Profile is one registered profile; Key doubles as its directory name under ~/.dot.
type Profile struct {
	Key  string
	Repo string
}

// Registry keeps profiles in file order: the order drives merges and reports.
type Registry struct {
	Default  string
	Profiles []Profile
}

type profileBody struct {
	Repo string `json:"repo"`
}

type fileFormat struct {
	Default  string          `json:"default"`
	Profiles orderedProfiles `json:"profiles"`
}

type orderedProfiles []Profile

func (o *orderedProfiles) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return errors.New("profiles : objet attendu")
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		key := t.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		body := json.NewDecoder(bytes.NewReader(raw))
		body.DisallowUnknownFields()
		var pb profileBody
		if err := body.Decode(&pb); err != nil {
			return fmt.Errorf("profiles.%s : %w", key, err)
		}
		*o = append(*o, Profile{Key: key, Repo: pb.Repo})
	}
	return nil
}

// Load reads and validates the registry; a missing file is an empty registry.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Registry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registre : %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f fileFormat
	err = dec.Decode(&f)
	if err == nil {
		if _, perr := dec.Token(); perr != io.EOF {
			err = errors.New("données après le premier objet")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("registre : %s : %w", path, err)
	}
	r := &Registry{Default: f.Default, Profiles: f.Profiles}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("registre : %s : %w", path, err)
	}
	return r, nil
}

// Validate checks keys, repos and the default profile.
func (r *Registry) Validate() error {
	seen := map[string]bool{}
	for _, p := range r.Profiles {
		if err := validKey(p.Key); err != nil {
			return err
		}
		if seen[p.Key] {
			return fmt.Errorf("profil en double : %s", p.Key)
		}
		seen[p.Key] = true
		if err := validRepo(p.Key, p.Repo); err != nil {
			return err
		}
	}
	switch {
	case len(r.Profiles) == 0 && r.Default != "":
		return fmt.Errorf("default : profil inconnu : %s", r.Default)
	case len(r.Profiles) > 0 && !seen[r.Default]:
		if r.Default == "" {
			return errors.New("default requis tant qu'un profil existe")
		}
		return fmt.Errorf("default : profil inconnu : %s", r.Default)
	}
	return nil
}

func validKey(key string) error {
	if !keyRe.MatchString(key) || strings.Contains(key, "..") {
		return fmt.Errorf("clé de profil invalide : %q", key)
	}
	return nil
}

func validRepo(key, repo string) error {
	if repo == "" || strings.IndexFunc(repo, unicode.IsControl) >= 0 {
		return fmt.Errorf("profiles.%s.repo : valeur vide ou invalide", key)
	}
	return nil
}

// Keys returns the profile keys in registry order.
func (r *Registry) Keys() []string {
	keys := make([]string, len(r.Profiles))
	for i, p := range r.Profiles {
		keys[i] = p.Key
	}
	return keys
}

func (r *Registry) index(key string) int {
	for i, p := range r.Profiles {
		if p.Key == key {
			return i
		}
	}
	return -1
}

// Add registers a profile; the first one becomes the default.
func (r *Registry) Add(key, repo string) error {
	if err := validKey(key); err != nil {
		return err
	}
	if err := validRepo(key, repo); err != nil {
		return err
	}
	if r.index(key) >= 0 {
		return fmt.Errorf("profil déjà inscrit : %s", key)
	}
	r.Profiles = append(r.Profiles, Profile{Key: key, Repo: repo})
	if r.Default == "" {
		r.Default = key
	}
	return nil
}

// Remove drops a profile; the default can only go when it is the last one.
func (r *Registry) Remove(key string) error {
	i := r.index(key)
	if i < 0 {
		return fmt.Errorf("%w : %s", ErrUnknownProfile, key)
	}
	if key == r.Default && len(r.Profiles) > 1 {
		return fmt.Errorf("%s est le profil par défaut : en choisir un autre d'abord (dot config set default <clé>)", key)
	}
	r.Profiles = append(r.Profiles[:i], r.Profiles[i+1:]...)
	if len(r.Profiles) == 0 {
		r.Default = ""
	}
	return nil
}

// parseKey understands the dotted keys of dot config: default, profiles.<clé>.repo.
func parseKey(key string) (profile string, isDefault bool, err error) {
	if key == "default" {
		return "", true, nil
	}
	if rest, ok := strings.CutPrefix(key, "profiles."); ok {
		if p, ok := strings.CutSuffix(rest, ".repo"); ok && p != "" {
			return p, false, nil
		}
	}
	return "", false, fmt.Errorf("%w : %s (attendu : default, profiles.<clé>.repo)", ErrUnknownKey, key)
}

// ProfileKey extracts <clé> from profiles.<clé>.repo.
func ProfileKey(key string) (string, bool) {
	p, isDefault, err := parseKey(key)
	return p, err == nil && !isDefault
}

func (r *Registry) Get(key string) (string, error) {
	p, isDefault, err := parseKey(key)
	if err != nil {
		return "", err
	}
	if isDefault {
		return r.Default, nil
	}
	i := r.index(p)
	if i < 0 {
		return "", fmt.Errorf("%w : %s", ErrUnknownProfile, p)
	}
	return r.Profiles[i].Repo, nil
}

// Set writes a value; profiles.<clé>.repo on an unknown key adds the profile.
func (r *Registry) Set(key, value string) error {
	p, isDefault, err := parseKey(key)
	if err != nil {
		return err
	}
	if isDefault {
		if r.index(value) < 0 {
			return fmt.Errorf("default : %w : %s", ErrUnknownProfile, value)
		}
		r.Default = value
		return nil
	}
	i := r.index(p)
	if i < 0 {
		return r.Add(p, value)
	}
	if err := validRepo(p, value); err != nil {
		return err
	}
	r.Profiles[i].Repo = value
	return nil
}

// Unset clears default (only without profiles) or removes a profile.
func (r *Registry) Unset(key string) error {
	p, isDefault, err := parseKey(key)
	if err != nil {
		return err
	}
	if !isDefault {
		return r.Remove(p)
	}
	if len(r.Profiles) > 0 {
		return errors.New("default requis tant qu'un profil existe")
	}
	return nil
}

// JSON renders the registry indented, profiles in order.
func (r *Registry) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *Registry) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	str := func(s string) {
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(s)
		buf.Truncate(buf.Len() - 1)
	}
	buf.WriteByte('{')
	if r.Default != "" {
		buf.WriteString(`"default":`)
		str(r.Default)
		buf.WriteByte(',')
	}
	buf.WriteString(`"profiles":{`)
	for i, p := range r.Profiles {
		if i > 0 {
			buf.WriteByte(',')
		}
		str(p.Key)
		buf.WriteString(`:{"repo":`)
		str(p.Repo)
		buf.WriteByte('}')
	}
	buf.WriteString("}}")
	return buf.Bytes(), nil
}

// Save validates, then writes through a temporary file renamed over the target (0600, dir 0700).
func (r *Registry) Save(path string) (err error) {
	if err := r.Validate(); err != nil {
		return err
	}
	data, err := r.JSON()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".profiles-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
