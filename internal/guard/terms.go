// Package guard is the leak guard: forbidden terms (a local, never-versioned list) and secrets
// (betterleaks). It fails closed and reports file names, refs or commits, never the matched text.
package guard

import (
	"bytes"
	"errors"
	"os"
	"regexp"
	"strings"
)

// ErrAbsent and ErrInvalid tell the two ways a list can be refused; neither carries a pattern.
var (
	ErrAbsent  = errors.New("absente ou vide")
	ErrInvalid = errors.New("invalide ou illisible")
)

// Terms matches text line by line against case-insensitive extended regular expressions.
// A nil *Terms matches nothing (secrets-only mode).
type Terms struct{ re *regexp.Regexp }

// ParseTerms reads one pattern per line; blank lines and lines starting with # are ignored.
// ponytail: RE2 stands in for POSIX ERE; what RE2 cannot compile (backreferences, \<) is refused,
// never skipped, and a pattern is never echoed back in an error.
func ParseTerms(data []byte) (*Terms, error) {
	var parts []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r") // a CRLF list would otherwise never match
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		// Each line is compiled alone: wrapping first could make "a)(b" look valid.
		if _, err := regexp.Compile("(?i)" + line); err != nil {
			return nil, ErrInvalid
		}
		parts = append(parts, "(?:"+line+")")
	}
	if len(parts) == 0 {
		return nil, ErrAbsent
	}
	re, err := regexp.Compile("(?i)" + strings.Join(parts, "|"))
	if err != nil {
		return nil, ErrInvalid
	}
	return &Terms{re: re}, nil
}

// LoadTerms reads the list at path; an unreadable file counts as absent.
func LoadTerms(path string) (*Terms, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrAbsent
	}
	return ParseTerms(data)
}

// MatchLine reports whether one line (no newline) matches a term.
func (t *Terms) MatchLine(line []byte) bool { return t != nil && t.re.Match(line) }

// Match reports whether any line of s matches a term (a pattern never spans lines, like grep).
func (t *Terms) Match(s string) bool {
	if t == nil {
		return false
	}
	for _, l := range bytes.Split([]byte(s), []byte{'\n'}) {
		if t.re.Match(l) {
			return true
		}
	}
	return false
}
