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
var ErrNoFile = errors.New("secrets.local file missing or unreadable")

// Mapping binds an environment variable name to a backend reference.
type Mapping struct {
	Name string // environment variable name
	Ref  string // bw:<item>[#field] or pass:<vault>/<item>[#field]
}

// Backend is "bw" or "pass".
func (m Mapping) Backend() string { b, _, _ := strings.Cut(m.Ref, ":"); return b }

// Item is the reference without its backend prefix.
func (m Mapping) Item() string { _, i, _ := strings.Cut(m.Ref, ":"); return i }

// Vault and Title split a pass reference at its first slash; a trailing #field is not part of the title.
func (m Mapping) Vault() string { v, _, _ := strings.Cut(m.Item(), "/"); return v }
func (m Mapping) Title() string {
	_, t, _ := strings.Cut(m.Item(), "/")
	t, _, _ = cutLast(t, "#")
	return t
}

// PassView is the pass-cli arguments that read the field of a pass reference. A vault or an item
// written as its id (the Share ID, the Item ID) is looked up by id: a title shared by several
// items is otherwise resolved to one of them, unpredictably.
func (m Mapping) PassView() []string {
	args := append(m.passVault(), "--item-title", m.Title())
	if looksLikeID(m.Title()) {
		args = append(m.passVault(), "--item-id", m.Title())
	}
	return append(append([]string{"item", "view"}, args...), "--field", m.Field())
}

func (m Mapping) passVault() []string {
	if looksLikeID(m.Vault()) {
		return []string{"--share-id", m.Vault()}
	}
	return []string{"--vault-name", m.Vault()}
}

// looksLikeID recognizes a Proton Pass id: 88 characters of base64url ending with "==".
func looksLikeID(s string) bool {
	if len(s) != 88 || !strings.HasSuffix(s, "==") {
		return false
	}
	for _, c := range s[:86] {
		if c != '-' && c != '_' && (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// BwItem is the Bitwarden item (name or id) without its #field suffix.
func (m Mapping) BwItem() string { i, _, _ := cutLast(m.Item(), "#"); return i }

// Field is the field to read: the #field suffix of the reference (an API key item keeps its
// key in a custom field), else "password".
func (m Mapping) Field() string {
	if f, ok := m.suffix(); ok {
		return f
	}
	return "password"
}

// HasField tells a reference with a #field suffix from one without, even when the field is "password".
func (m Mapping) HasField() bool { _, ok := m.suffix(); return ok }

func (m Mapping) suffix() (string, bool) {
	t := m.Item()
	if m.Backend() == "pass" {
		_, t, _ = strings.Cut(t, "/")
	}
	_, f, ok := cutLast(t, "#")
	return f, ok
}

func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

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
		item, f, has := cutLast(strings.TrimPrefix(ref, "bw:"), "#")
		return item != "" && (!has || f != "")
	case strings.HasPrefix(ref, "pass:"):
		vault, title, ok := strings.Cut(strings.TrimPrefix(ref, "pass:"), "/")
		if t, f, has := cutLast(title, "#"); has {
			title = t
			ok = ok && f != ""
		}
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
		ref = unquote(ref)
		if !ok || !Valid(name, ref) {
			return nil, fmt.Errorf("line %d malformed", i+1)
		}
		for _, old := range ms {
			if old.Name == name {
				return nil, fmt.Errorf("line %d: duplicate NAME", i+1)
			}
		}
		ms = append(ms, Mapping{name, ref})
	}
	return ms, nil
}

// unquote removes one pair of surrounding quotes ("…" or '…'), the usual way to write a value
// holding spaces or a #. A reference without quotes stays valid.
func unquote(ref string) string {
	if n := len(ref); n >= 2 && (ref[0] == '"' || ref[0] == '\'') && ref[n-1] == ref[0] {
		return ref[1 : n-1]
	}
	return ref
}

// Line is the mapping as written to secrets.local: quoted when the reference holds a space or a #.
func (m Mapping) Line() string {
	if strings.ContainsAny(m.Ref, " \t#") && !strings.Contains(m.Ref, `"`) {
		return m.Name + `="` + m.Ref + `"`
	}
	return m.Name + "=" + m.Ref
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
	return Mapping{}, fmt.Errorf("%s: unknown NAME", name)
}
