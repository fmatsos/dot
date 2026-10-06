package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fmatsos/dot/internal/registry"
)

// Env carries everything a command touches, so tests can swap it for temporary ones.
type Env struct {
	Home    string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Profile string // value of -p/--profile
}

func (e *Env) Getenv(k string) string { return os.Getenv(k) }

// Expand turns ~ and ~/… into paths under Home.
func (e *Env) Expand(p string) string {
	if p == "~" {
		return e.Home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(e.Home, rest)
	}
	return p
}

func (e *Env) RegistryPath() string { return filepath.Join(e.Home, ".dot", "profiles.json") }

// ProfileDirFor is the clone directory of a registered profile key.
func (e *Env) ProfileDirFor(key string) string { return filepath.Join(e.Home, ".dot", key) }

func (e *Env) Registry() (*registry.Registry, error) { return registry.Load(e.RegistryPath()) }

// ProfileKeys lists the registered profile keys in registry order.
func (e *Env) ProfileKeys() ([]string, error) {
	r, err := e.Registry()
	if err != nil {
		return nil, err
	}
	return r.Keys(), nil
}

// ProfileDir resolves the targeted profile: $DOTFILES_DEPLOY as is, else -p, $DOT_PROFILE, registry default.
func (e *Env) ProfileDir() (string, error) {
	if d := e.Getenv("DOTFILES_DEPLOY"); d != "" {
		return e.Expand(d), nil
	}
	r, err := e.Registry()
	if err != nil {
		return "", err
	}
	if len(r.Profiles) == 0 {
		return "", errors.New("aucun profil inscrit (dot install <url>)")
	}
	key := cmp.Or(e.Profile, e.Getenv("DOT_PROFILE"), r.Default)
	if !slices.Contains(r.Keys(), key) {
		return "", fmt.Errorf("profil inconnu : %s (profils inscrits : %s)", key, strings.Join(r.Keys(), ", "))
	}
	return e.ProfileDirFor(key), nil
}
