// Package mcp shares one list of MCP servers between Claude Code, Codex and OpenCode.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
)

var (
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)
	envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ErrSource marks an invalid source file or an unknown secret name.
var ErrSource = errors.New("source invalide ou NOM de secret inconnu")

// Server is one desired server in the form every tool receives: stdio (Command, Args, Env) or HTTP (URL).
type Server struct {
	URL     string
	Command string
	Args    []string
	Env     map[string]string
}

func (s *Server) http() bool { return s.URL != "" }

// Value is the canonical entry used to compare with what a tool reports.
func (s *Server) Value() any {
	if s == nil {
		return nil
	}
	if s.http() {
		return map[string]any{"type": "http", "url": s.URL}
	}
	return map[string]any{"type": "stdio", "command": s.Command, "args": s.Args, "env": s.Env}
}

// JSON renders the entry for `claude mcp add-json`.
func (s *Server) JSON() string {
	if s.http() {
		return canon(struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		}{"http", s.URL})
	}
	return canon(struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}{"stdio", s.Command, s.Args, s.Env})
}

// decode reads exactly one JSON value, keeping numbers as written.
func decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON invalide")
	}
	return v, nil
}

// ParseSource decodes a servers.json / servers.local.json file: one JSON object.
func ParseSource(data []byte) (map[string]any, error) {
	v, err := decode(data)
	if err != nil {
		return nil, ErrSource
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrSource
	}
	return m, nil
}

// canon is compact JSON with sorted keys, the form entries are compared in (jq -Sc).
func canon(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

func text(v any) bool {
	s, ok := v.(string)
	return ok && s != "" && !strings.ContainsRune(s, 0)
}

func strList(v any) ([]string, bool) {
	l, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(l))
	for _, e := range l {
		s, ok := e.(string)
		if !ok || strings.ContainsRune(s, 0) {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// parseEntry validates one server entry and converts it; a nil Server is an explicit removal.
// owner maps the secret names of a server to the key of the profile that declares them all.
func parseEntry(v any, local bool, owner func(names []string) (key string, ok bool), dot string) (*Server, bool) {
	if v == nil {
		return nil, local
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	_, hasCmd := o["command"]
	_, hasURL := o["url"]
	if hasCmd == hasURL {
		return nil, false
	}
	if hasURL {
		if !text(o["url"]) || len(o) != 1 {
			return nil, false
		}
		return &Server{URL: o["url"].(string)}, true
	}
	if !text(o["command"]) {
		return nil, false
	}
	for k := range o {
		if !slices.Contains([]string{"command", "args", "env", "secrets"}, k) {
			return nil, false
		}
	}
	s := &Server{Command: o["command"].(string), Args: []string{}, Env: map[string]string{}}
	if a, has := o["args"]; has {
		if s.Args, ok = strList(a); !ok {
			return nil, false
		}
	}
	if e, has := o["env"]; has {
		em, ok := e.(map[string]any)
		if !ok {
			return nil, false
		}
		for k, val := range em {
			str, ok := val.(string)
			if !envKeyRe.MatchString(k) || !ok || strings.ContainsRune(str, 0) {
				return nil, false
			}
			s.Env[k] = str
		}
	}
	if sec, has := o["secrets"]; has {
		names, ok := strList(sec)
		if !ok {
			return nil, false
		}
		if len(names) > 0 {
			key, ok := owner(names)
			if !ok {
				return nil, false
			}
			// The secret values are only read by `dot secrets run`, when the server starts, in the
			// profile that declares them: -p goes before the command, where the dispatcher strips it.
			var args []string
			if key != "" {
				args = []string{"-p", key}
			}
			args = append(append(args, "secrets", "run"), names...)
			args = append(args, "--", s.Command)
			s.Args = append(args, s.Args...)
			s.Command = dot
		}
	}
	return s, true
}

// Owner is a profile as Convert sees it: the key `dot -p` takes ("" when it needs no -p) and the
// secret names (never values) its secrets.local declares.
type Owner struct {
	Key   string
	Known map[string]bool
}

// Source is a shared servers file. Owner is the profile it comes from; nil when no profile owns it
// (the linked ~/.config/mcp/servers.json): its secrets are then looked up like those of the local file.
type Source struct {
	Servers map[string]any
	Owner   *Owner
}

// Convert validates every source file (including entries a later source overrides) and returns
// the desired servers by name. shared files merge in order (the last wins), then local replaces
// whole entries by name; a nil Server (explicit null in local) removes the name.
// dot is the absolute path of the dot binary that wraps servers using secrets. The secret names of
// a server must all be declared by its own profile; a server with no owner (local file, linked
// file) takes the first of owners that declares them all, and is refused when there is none.
func Convert(shared []Source, local map[string]any, owners []Owner, dot string) (map[string]*Server, error) {
	out := map[string]*Server{}
	srcs := append(slices.Clone(shared), Source{Servers: local})
	for i, src := range srcs {
		isLocal := i == len(shared)
		cands := owners
		if src.Owner != nil {
			cands = []Owner{*src.Owner}
		}
		spread := false
		owner := func(names []string) (string, bool) {
			declares := func(o Owner) bool {
				return !slices.ContainsFunc(names, func(n string) bool { return !o.Known[n] })
			}
			if k := slices.IndexFunc(cands, declares); k >= 0 {
				return cands[k].Key, true
			}
			// Every name is known, but no single profile declares them all.
			spread = src.Owner == nil && !slices.ContainsFunc(names, func(n string) bool {
				return !slices.ContainsFunc(cands, func(o Owner) bool { return o.Known[n] })
			})
			return "", false
		}
		for name, raw := range src.Servers {
			s, ok := parseEntry(raw, isLocal, owner, dot)
			if !ok || !nameRe.MatchString(name) {
				if spread {
					return nil, fmt.Errorf("%w : serveur %s, ses secrets ne sont pas tous déclarés par un même profil", ErrSource, name)
				}
				return nil, ErrSource
			}
			out[name] = s
		}
	}
	return out, nil
}

// sortedNames lists the keys of m in code point order, the order plans and reports use.
func sortedNames[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
