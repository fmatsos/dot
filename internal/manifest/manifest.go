// Package manifest reads and validates a profile's dot.json; unknown keys are refused everywhere.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
)

var (
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// A sparse entry is a top-level name; home@<name> is the variant of home/ (see internal/link).
	sparseRe  = regexp.MustCompile(`^([A-Za-z0-9_.][A-Za-z0-9_.-]*|home@[A-Za-z0-9][A-Za-z0-9_.-]*)$`)
	nodeRe    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	packageRe = regexp.MustCompile(`^(@[A-Za-z0-9._-]+/)?[A-Za-z0-9][A-Za-z0-9._-]*@[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

// KeyError names the first offending key; "objet" stands for an unreadable or non-object file.
type KeyError struct {
	File string
	Key  string
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("manifest : %s : clé %s invalide ou fichier absent", e.File, e.Key)
}

type ProfilePath struct {
	Name string
	Path string
}

type Marketplace struct {
	Name    string
	Plugins []string
}

type NVM struct {
	Node     string
	Packages []string
}

// Manifest is a validated dot.json; Marketplace and NVM are nil when absent.
type Manifest struct {
	Repo          string
	Sparse        []string
	Profiles      []ProfilePath
	DeployProfile string
	Marketplace   *Marketplace
	NVM           *NVM
	Modules       []string
}

// Load reads and validates the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &KeyError{File: path, Key: "objet"}
	}
	m, key := parse(data)
	if key != "" {
		return nil, &KeyError{File: path, Key: key}
	}
	return m, nil
}

type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func (o *object) get(k string) json.RawMessage { return o.vals[k] }

// parseObject keeps key order; a repeated key keeps its first position and its last value.
func parseObject(raw json.RawMessage) (*object, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	o := &object{vals: map[string]json.RawMessage{}}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, false
		}
		k := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		if _, dup := o.vals[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = v
	}
	return o, true
}

func (o *object) unknown(prefix string, allowed ...string) string {
	for _, k := range o.keys {
		if !slices.Contains(allowed, k) {
			return prefix + k
		}
	}
	return ""
}

func asString(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// asText is a non-empty string without control characters.
func asText(raw json.RawMessage) (string, bool) {
	s, ok := asString(raw)
	if !ok || s == "" || strings.IndexFunc(s, func(r rune) bool { return r < 0x20 }) >= 0 {
		return "", false
	}
	return s, true
}

func asNamed(raw json.RawMessage) (string, bool) {
	s, ok := asText(raw)
	return s, ok && nameRe.MatchString(s)
}

func asArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	var a []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &a) != nil {
		return nil, false
	}
	return a, true
}

// asStrings validates each element with check; it fails on the first rejected one.
func asStrings(raw json.RawMessage, check func(string) bool) ([]string, bool) {
	a, ok := asArray(raw)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(a))
	for _, e := range a {
		s, ok := asText(e)
		if !ok || !check(s) {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// parse returns the offending key, in the order the bash reader checked them.
func parse(data []byte) (*Manifest, string) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var doc json.RawMessage
	if err := dec.Decode(&doc); err != nil {
		return nil, "objet"
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, "objet"
	}
	root, ok := parseObject(doc)
	if !ok {
		return nil, "objet"
	}
	if k := root.unknown("", "repo", "deploy", "profiles", "deployProfile", "marketplace", "nvm", "modules"); k != "" {
		return nil, k
	}
	m := &Manifest{}
	if m.Repo, ok = asText(root.get("repo")); !ok {
		return nil, "repo"
	}
	deploy, ok := parseObject(root.get("deploy"))
	if !ok {
		return nil, "deploy"
	}
	if k := deploy.unknown("deploy.", "sparse"); k != "" {
		return nil, k
	}
	if m.Sparse, ok = asStrings(deploy.get("sparse"), func(s string) bool {
		return sparseRe.MatchString(s) && s != "." && !strings.Contains(s, "..")
	}); !ok || len(m.Sparse) == 0 {
		return nil, "deploy.sparse"
	}
	profiles, ok := parseObject(root.get("profiles"))
	if !ok || len(profiles.keys) == 0 {
		return nil, "profiles"
	}
	for _, name := range profiles.keys {
		p, ok := asText(profiles.get(name))
		if !nameRe.MatchString(name) || !ok || !strings.HasPrefix(p, "home/") || !strings.HasSuffix(p, ".gitconfig") || strings.Contains(p, "..") {
			return nil, "profiles." + name
		}
		m.Profiles = append(m.Profiles, ProfilePath{Name: name, Path: p})
	}
	if m.DeployProfile, ok = asText(root.get("deployProfile")); !ok || profiles.vals[m.DeployProfile] == nil {
		return nil, "deployProfile"
	}
	if m.Modules, ok = asStrings(root.get("modules"), func(s string) bool { return s == "settings" || s == "mcp" }); !ok ||
		len(slices.Compact(slices.Sorted(slices.Values(m.Modules)))) != len(m.Modules) { // sorted copy: duplicates become adjacent
		return nil, "modules"
	}
	if raw, has := root.vals["marketplace"]; has {
		mk, ok := parseObject(raw)
		if !ok {
			return nil, "marketplace"
		}
		if k := mk.unknown("marketplace.", "name", "plugins"); k != "" {
			return nil, k
		}
		m.Marketplace = &Marketplace{}
		if m.Marketplace.Name, ok = asNamed(mk.get("name")); !ok {
			return nil, "marketplace.name"
		}
		if m.Marketplace.Plugins, ok = asStrings(mk.get("plugins"), nameRe.MatchString); !ok {
			return nil, "marketplace.plugins"
		}
	}
	if raw, has := root.vals["nvm"]; has {
		nv, ok := parseObject(raw)
		if !ok {
			return nil, "nvm"
		}
		if k := nv.unknown("nvm.", "node", "packages"); k != "" {
			return nil, k
		}
		m.NVM = &NVM{}
		if m.NVM.Node, ok = asText(nv.get("node")); !ok || !nodeRe.MatchString(m.NVM.Node) {
			return nil, "nvm.node"
		}
		if m.NVM.Packages, ok = asStrings(nv.get("packages"), packageRe.MatchString); !ok {
			return nil, "nvm.packages"
		}
	}
	return m, ""
}
