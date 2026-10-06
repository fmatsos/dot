// Package guard is the leak guard: forbidden terms (a local, never-versioned list) and secrets
// (betterleaks). It fails closed and reports file names, refs or commits, never the matched text.
package guard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ErrAbsent and ErrInvalid tell the two ways a list can be refused; neither carries a pattern.
var (
	ErrAbsent  = errors.New("absente ou vide")
	ErrInvalid = errors.New("invalide ou illisible")
)

// ErrMatchesEmpty is an ErrInvalid: some pattern matches the empty string, hence every line.
var ErrMatchesEmpty = fmt.Errorf("%w : un terme correspond à la chaîne vide, donc à tout (a*, ^, x?…)", ErrInvalid)

// Terms matches text line by line against case-insensitive extended regular expressions.
// A nil *Terms matches nothing (secrets-only mode).
type Terms struct{ re *regexp.Regexp }

// ParseTerms reads one pattern per line; blank lines and lines starting with # are ignored.
// ponytail: RE2 stands in for POSIX ERE; what RE2 cannot compile (backreferences, \<) is refused,
// never skipped, and a pattern is never echoed back in an error.
func ParseTerms(data []byte) (*Terms, error) {
	var parts []string
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // a BOM would otherwise stick to the first pattern
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r") // a CRLF list would otherwise never match
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if gnuOnly(line) {
			return nil, ErrInvalid
		}
		// Each line is compiled alone: wrapping first could make "a)(b" look valid.
		// A pattern matching the empty string (a*, x?, ^) would block every line: refused.
		one, err := regexp.Compile("(?i)" + line)
		if err != nil {
			return nil, ErrInvalid
		}
		if one.MatchString("") {
			return nil, ErrMatchesEmpty
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

var caseOff = regexp.MustCompile(`\(\?[imsU]*-[imsU]*i`)

// gnuOnly reports a pattern RE2 would read differently from the GNU grep it was written for: an
// escaped operator (\| \+ \? \{ \( \)) or a flag group that turns case-insensitivity off. Such a
// pattern would be accepted and silently block nothing, so it is refused.
// ponytail: scans escapes without tracking bracket expressions, so [\(] is refused too.
func gnuOnly(p string) bool {
	if caseOff.MatchString(p) {
		return true
	}
	for i := 0; i < len(p); i++ {
		if p[i] != '\\' {
			continue
		}
		if i+1 < len(p) && strings.IndexByte("|+?{()", p[i+1]) >= 0 {
			return true
		}
		i++ // skip the escaped byte, so \\| is a literal backslash then an alternation
	}
	return false
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
