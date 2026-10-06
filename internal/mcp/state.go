package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
)

// Entries are the raw JSON values a tool holds for each server name (nil when absent).
// Unknown keys are kept: an entry carrying extra keys differs from the desired one.
type Entries map[string]any

// setDefault mimics jq's `.k //= v`: it replaces an absent, null or false value.
func setDefault(m map[string]any, k string, v any) {
	if cur, ok := m[k]; !ok || cur == nil || cur == false {
		m[k] = v
	}
}

// orDefault mimics jq's `v // d`.
func orDefault(v, d any) any {
	if v == nil || v == false {
		return d
	}
	return v
}

// ClaudeEntries reads `.mcpServers` of ~/.claude.json, normalised to the shape `claude mcp add-json` stores.
func ClaudeEntries(data []byte) (Entries, error) {
	v, err := decode(data)
	if err != nil {
		return nil, err
	}
	top, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("config")
	}
	servers, ok := orDefault(top["mcpServers"], map[string]any{}).(map[string]any)
	if !ok {
		return nil, errors.New("config")
	}
	out := Entries{}
	for name, raw := range servers {
		e, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("config")
		}
		e = maps.Clone(e)
		if _, has := e["command"]; has {
			setDefault(e, "type", "stdio")
			setDefault(e, "args", []any{})
			setDefault(e, "env", map[string]any{})
		} else if e["type"] == "streamable-http" {
			e["type"] = "http"
		}
		out[name] = e
	}
	return out, nil
}

// CodexEntries reads the output of `codex mcp list --json`, keeping only the transport.
func CodexEntries(data []byte) (Entries, error) {
	v, err := decode(data)
	if err != nil {
		return nil, err
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("config")
	}
	out := Entries{}
	for _, raw := range list {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("config")
		}
		name, ok := item["name"].(string)
		if !ok {
			return nil, errors.New("config")
		}
		switch t := item["transport"].(type) {
		case nil:
			out[name] = nil
		case map[string]any:
			switch t["type"] {
			case "stdio":
				out[name] = map[string]any{"type": "stdio", "command": t["command"],
					"args": orDefault(t["args"], []any{}), "env": orDefault(t["env"], map[string]any{})}
			case "streamable_http":
				out[name] = map[string]any{"type": "http", "url": t["url"]}
			default:
				out[name] = t
			}
		default:
			return nil, errors.New("config")
		}
	}
	return out, nil
}

// ParseOpenCode reads the generated OpenCode config: an object whose `mcp` is an object when present.
func ParseOpenCode(data []byte) (map[string]any, error) {
	v, err := decode(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("config")
	}
	if _, ok := orDefault(m["mcp"], map[string]any{}).(map[string]any); !ok {
		return nil, errors.New("config")
	}
	return m, nil
}

func openServers(cfg map[string]any) map[string]any {
	return orDefault(cfg["mcp"], map[string]any{}).(map[string]any)
}

// RenderOpenCode returns cfg with the desired servers applied to its `mcp` object, every other key untouched.
func RenderOpenCode(cfg map[string]any, desired map[string]*Server) map[string]any {
	out := maps.Clone(cfg)
	servers := maps.Clone(openServers(cfg))
	for name, s := range desired {
		switch {
		case s == nil:
			delete(servers, name)
		case s.http():
			servers[name] = map[string]any{"type": "remote", "url": s.URL, "enabled": true}
		default:
			servers[name] = map[string]any{"type": "local", "command": append([]string{s.Command}, s.Args...),
				"environment": s.Env, "enabled": true}
		}
	}
	out["mcp"] = servers
	return out
}

// Step is one planned change: action is "ajout", "modification" or "retrait".
type Step struct{ Tool, Action, Name string }

// Plan compares the wanted entries (nil = absent) with a tool's current ones, by name in code point order.
func Plan(tool string, want map[string]any, current Entries) []Step {
	var steps []Step
	for _, name := range sortedNames(want) {
		w, c := want[name], current[name]
		if canon(w) == canon(c) {
			continue
		}
		action := "modification"
		if w == nil {
			action = "retrait"
		} else if c == nil {
			action = "ajout"
		}
		steps = append(steps, Step{tool, action, name})
	}
	return steps
}

// Pretty is the written form of the OpenCode config: sorted keys, two spaces, final newline.
func Pretty(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return b.Bytes(), err
}
