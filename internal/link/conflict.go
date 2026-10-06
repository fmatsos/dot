package link

import (
	"fmt"
	"slices"
	"strings"
)

// Profile names a profile clone for conflict detection; order is registry order.
type Profile struct{ Key, Dir string }

// Conflict is one target claimed by several profiles; Keys and Srcs are in profile order.
type Conflict struct {
	Dst  string
	Keys []string
	Srcs []string
}

// ConflictError is the refusal returned by Conflicts; its message names every source path.
type ConflictError struct {
	Home      string
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	var b strings.Builder
	b.WriteString("installation refusée : fichier lié par plusieurs profils")
	for _, c := range e.Conflicts {
		fmt.Fprintf(&b, "\n  %s :", Tilde(e.Home, c.Dst))
		for i := range c.Keys {
			fmt.Fprintf(&b, " %s (%s)", c.Keys[i], c.Srcs[i])
		}
	}
	return b.String()
}

// Conflicts finds targets (home/ files and bin/ names) linked by more than one profile.
// It returns the conflicts sorted by Dst and, when there are any, a *ConflictError.
func Conflicts(home string, profiles []Profile) ([]Conflict, error) {
	byDst := map[string]*Conflict{}
	for _, p := range profiles {
		plan, err := Plan(p.Dir, home)
		if err != nil {
			return nil, fmt.Errorf("profil %s : %w", p.Key, err)
		}
		for _, k := range plan {
			c := byDst[k.Dst]
			if c == nil {
				c = &Conflict{Dst: k.Dst}
				byDst[k.Dst] = c
			}
			c.Keys = append(c.Keys, p.Key)
			c.Srcs = append(c.Srcs, k.Src)
		}
	}
	var out []Conflict
	for _, c := range byDst {
		if len(c.Keys) > 1 {
			out = append(out, *c)
		}
	}
	if out == nil {
		return nil, nil
	}
	slices.SortFunc(out, func(a, b Conflict) int { return strings.Compare(a.Dst, b.Dst) })
	return out, &ConflictError{home, out}
}
