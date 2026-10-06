// Package secrets reads secrets on demand from Bitwarden (bw) and Proton Pass (pass-cli),
// following the mappings of a profile's secrets.local. Values never reach argv or the terminal:
// they travel through a pipe, stdin or the environment of one process.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File is the mapping file name inside a profile directory.
const File = "secrets.local"

// ErrNoFile means secrets.local is missing or unreadable.
var ErrNoFile = errors.New("fichier secrets.local absent ou illisible")

// Mapping binds an environment variable name to a backend reference.
type Mapping struct {
	Name string // environment variable name
	Ref  string // bw:<item> or pass:<vault>/<item>
}

// Backend is "bw" or "pass".
func (m Mapping) Backend() string { b, _, _ := strings.Cut(m.Ref, ":"); return b }

// Item is the reference without its backend prefix.
func (m Mapping) Item() string { _, i, _ := strings.Cut(m.Ref, ":"); return i }

// Vault and Title split a pass reference at its first slash.
func (m Mapping) Vault() string { v, _, _ := strings.Cut(m.Item(), "/"); return v }
func (m Mapping) Title() string { _, t, _ := strings.Cut(m.Item(), "/"); return t }

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// Valid reports whether name and ref form an acceptable mapping.
func Valid(name, ref string) bool {
	if !validName(name) || strings.ContainsAny(ref, "\r\n") {
		return false
	}
	switch {
	case strings.HasPrefix(ref, "bw:"):
		return len(ref) > len("bw:")
	case strings.HasPrefix(ref, "pass:"):
		vault, title, ok := strings.Cut(strings.TrimPrefix(ref, "pass:"), "/")
		return ok && vault != "" && title != ""
	}
	return false
}

// Parse reads the content of a secrets.local: blank lines and # comments are skipped,
// anything else must be a valid, non-duplicate NAME=ref mapping. Errors carry the line number
// only, never the text of the line.
func Parse(data []byte) ([]Mapping, error) {
	var ms []Mapping
	lines := strings.Split(string(data), "\n")
	if n := len(lines); lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, line := range lines {
		if t := strings.TrimSpace(line); t == "" || t[0] == '#' {
			continue
		}
		name, ref, ok := strings.Cut(line, "=")
		if !ok || !Valid(name, ref) {
			return nil, fmt.Errorf("ligne %d mal formée", i+1)
		}
		for _, old := range ms {
			if old.Name == name {
				return nil, fmt.Errorf("ligne %d : NAME en double", i+1)
			}
		}
		ms = append(ms, Mapping{name, ref})
	}
	return ms, nil
}

// Load parses dir/secrets.local.
func Load(dir string) ([]Mapping, error) {
	data, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		return nil, ErrNoFile
	}
	return Parse(data)
}

// Lookup finds a mapping by name.
func Lookup(ms []Mapping, name string) (Mapping, error) {
	for _, m := range ms {
		if m.Name == name {
			return m, nil
		}
	}
	return Mapping{}, fmt.Errorf("%s : NAME inconnu", name)
}
