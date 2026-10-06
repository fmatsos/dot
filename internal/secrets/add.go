package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrMalformed means the NAME or the reference given to Add is not a valid mapping.
var ErrMalformed = errors.New("NOM ou référence mal formé")

// Add registers NAME=ref in secrets.local. An existing manager item is reused (created is false,
// input is never called); otherwise input supplies the value, the item is created, read back and
// compared in memory, and only then is the mapping written (0600, atomically).
func (s *Store) Add(ctx context.Context, name, ref string, input func() (string, error)) (created bool, err error) {
	if !Valid(name, ref) {
		return false, ErrMalformed
	}
	if _, err := os.Stat(filepath.Join(s.Dir, File)); err == nil {
		ms, err := Load(s.Dir)
		if err != nil {
			return false, err
		}
		if _, err := Lookup(ms, name); err == nil {
			return false, fmt.Errorf("%s : NAME déjà enregistré", name)
		}
	}
	m := Mapping{name, ref}
	exists, err := s.exists(ctx, m)
	if err != nil {
		return false, err
	}
	if !exists {
		value, err := input()
		if err != nil {
			return false, err
		}
		if value == "" {
			return false, errors.New("valeur vide")
		}
		if err := s.create(ctx, m, value); err != nil {
			return false, err
		}
		// Compare in memory: neither the value nor a manager's error reaches the terminal.
		back, err := s.Read(ctx, m)
		if err != nil {
			return false, errors.New("élément créé mais non enregistré : relecture impossible")
		}
		if back != value {
			return false, errors.New("élément créé mais non enregistré : valeur différente à la relecture")
		}
	}
	return !exists, s.register(m)
}

// exists looks the item up; Bitwarden must be unlocked and Proton Pass logged in.
func (s *Store) exists(ctx context.Context, m Mapping) (bool, error) {
	if m.Backend() == "bw" {
		sess, err := s.unlockedSession(ctx)
		if err != nil {
			return false, err
		}
		out, err := s.run(ctx, []string{"BW_SESSION=" + sess}, nil, "bw", "list", "items", "--search", m.Item())
		var items []struct {
			Name string `json:"name"`
		}
		if err != nil || json.Unmarshal(out, &items) != nil {
			return false, errors.New("Bitwarden : recherche impossible")
		}
		switch {
		case len(items) == 0:
			return false, nil
		case len(items) == 1 && items[0].Name == m.Item():
			return true, nil
		}
		return false, errors.New("Bitwarden : recherche ambiguë pour bw get, choisis un nom plus précis")
	}
	if _, err := s.run(ctx, nil, nil, "pass-cli", "info"); err != nil {
		return false, ErrPassLoggedOut
	}
	_, err := s.run(ctx, nil, nil, "pass-cli", "item", "view", "--vault-name", m.Vault(), "--item-title", m.Title(), "--field", "password")
	return err == nil, nil
}

// template fetches a manager's JSON item template, lets set fill it, and returns the new JSON.
// The value only ever lives in this buffer and in the stdin of the next command.
func (s *Store) template(ctx context.Context, extra []string, set func(doc map[string]any), name string, args ...string) ([]byte, error) {
	out, err := s.run(ctx, extra, nil, name, args...)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber() // keep the template's other numbers verbatim
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil, errors.New("modèle illisible")
	}
	set(doc)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Store) create(ctx context.Context, m Mapping, value string) error {
	if m.Backend() == "bw" {
		session, err := s.unlockedSession(ctx) // cached by exists
		if err != nil {
			return err
		}
		sess := []string{"BW_SESSION=" + session}
		raw, err := s.template(ctx, sess, func(doc map[string]any) {
			doc["type"] = 1
			doc["name"] = m.Item()
			doc["notes"] = nil
			login, ok := doc["login"].(map[string]any)
			if !ok {
				login = map[string]any{}
			}
			login["password"] = value
			doc["login"] = login
		}, "bw", "get", "template", "item")
		if err == nil {
			var enc []byte
			if enc, err = s.run(ctx, sess, bytes.NewReader(raw), "bw", "encode"); err == nil {
				_, err = s.run(ctx, sess, bytes.NewReader(enc), "bw", "create", "item")
			}
		}
		if err != nil {
			return errors.New("Bitwarden : création impossible")
		}
		return nil
	}
	raw, err := s.template(ctx, nil, func(doc map[string]any) {
		doc["title"] = m.Title()
		doc["password"] = value
	}, "pass-cli", "item", "create", "login", "--get-template")
	if err == nil {
		_, err = s.run(ctx, nil, bytes.NewReader(raw), "pass-cli", "item", "create", "login", "--vault-name", m.Vault(), "--from-template", "-")
	}
	if err != nil {
		return errors.New("Proton Pass : création impossible")
	}
	return nil
}

// register appends the mapping to secrets.local, guaranteeing a newline before it.
func (s *Store) register(m Mapping) error {
	data, _ := os.ReadFile(filepath.Join(s.Dir, File))
	if n := len(data); n > 0 && data[n-1] != '\n' {
		data = append(data, '\n')
	}
	return writeAtomic(s.Dir, File, append(data, m.Name+"="+m.Ref+"\n"...))
}
