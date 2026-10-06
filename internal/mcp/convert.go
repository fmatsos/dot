// Package mcp shares one list of MCP servers between Claude Code, Codex and OpenCode.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
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
func parseEntry(v any, local bool, known map[string]bool, dot string) (*Server, bool) {
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
		for _, n := range names {
			if !known[n] {
				return nil, false
			}
		}
		if len(names) > 0 {
			// The secret values are only read by `dot secrets run`, when the server starts.
			args := append([]string{"secrets", "run"}, names...)
			args = append(args, "--", s.Command)
			s.Args = append(args, s.Args...)
			s.Command = dot
		}
	}
	return s, true
}

// Convert validates every source file (including entries a later source overrides) and returns
// the desired servers by name. shared files merge in order (the last wins), then local replaces
// whole entries by name; a nil Server (explicit null in local) removes the name.
// dot is the absolute path of the dot binary that wraps servers using secrets; known holds the
// secret names (never values) declared in secrets.local.
func Convert(shared []map[string]any, local map[string]any, known map[string]bool, dot string) (map[string]*Server, error) {
	out := map[string]*Server{}
	for i, src := range append(slices.Clone(shared), local) {
		isLocal := i == len(shared)
		for name, raw := range src {
			s, ok := parseEntry(raw, isLocal, known, dot)
			if !ok || !nameRe.MatchString(name) {
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
