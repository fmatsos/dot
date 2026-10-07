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

// Conflicts finds targets (home/ files and bin/ names) linked by more than one profile, or by one
// profile under a path another links as a file. With two
// profiles or more the ModuleSources are not linked, so they never conflict.
// It returns the conflicts sorted by Dst and, when there are any, a *ConflictError.
func Conflicts(home string, profiles []Profile) ([]Conflict, error) {
	byDst := map[string]*Conflict{}
	for _, p := range profiles {
		plan, err := PlanFor(p.Dir, home, len(profiles) > 1)
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
	dsts := make([]string, 0, len(byDst))
	for dst, c := range byDst {
		dsts = append(dsts, dst)
		if len(c.Keys) > 1 {
			out = append(out, *c)
		}
	}
	// A destination under another profile's destination (a file where the other one needs a
	// directory) cannot be linked by both: report the pair at the ancestor.
	slices.Sort(dsts)
	for i, anc := range dsts {
		for _, dst := range dsts[i+1:] {
			if !strings.HasPrefix(dst, anc) {
				break
			}
			if !strings.HasPrefix(dst, anc+"/") {
				continue
			}
			for ia, ka := range byDst[anc].Keys {
				for id, kd := range byDst[dst].Keys {
					if ka != kd {
						out = append(out, Conflict{Dst: anc, Keys: []string{ka, kd}, Srcs: []string{byDst[anc].Srcs[ia], byDst[dst].Srcs[id]}})
					}
				}
			}
		}
	}
	if out == nil {
		return nil, nil
	}
	slices.SortFunc(out, func(a, b Conflict) int { return strings.Compare(a.Dst, b.Dst) })
	return out, &ConflictError{home, out}
}
