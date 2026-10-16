package guard

import (
	"errors"
	"strings"
	"testing"
)

func TestParseTermsBOM(t *testing.T) {
	// A UTF-8 BOM (editors on Windows) must not glue itself to the first pattern.
	terms, err := ParseTerms([]byte("\xef\xbb\xbfacmecorp\r\nglobex\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("AcmeCorp") || !terms.Match("globex") {
		t.Error("the first term of a list with a BOM must match")
	}
	// A BOM then a comment, alone: still an empty list, refused.
	if _, err := ParseTerms([]byte("\xef\xbb\xbf# note\n")); !errors.Is(err, ErrAbsent) {
		t.Errorf("BOM + comment: %v", err)
	}
}

func TestParseTermsEmptyMatchRefused(t *testing.T) {
	// A pattern that matches the empty string matches every line: refused, whatever its neighbours.
	for _, p := range []string{"a*", "x?", "(|a)", "^", "$", "(?:)", "acmecorp|", "(acmecorp)?", "[a-z]*", `\b*`} {
		for _, in := range []string{p + "\n", "acmecorp\n" + p + "\n", "\xef\xbb\xbf" + p + "\r\n"} {
			terms, err := ParseTerms([]byte(in))
			if !errors.Is(err, ErrInvalid) || terms != nil {
				t.Errorf("%q : %v, %v", in, terms, err)
			}
			if err != nil && strings.Contains(err.Error(), "acmecorp") {
				t.Errorf("%q: the error quotes the pattern", in)
			}
		}
	}
	// Non-empty-matching patterns that look similar stay valid.
	for _, p := range []string{"a+", "a*b", "^a", "a$", "(acme)?corp"} {
		if _, err := ParseTerms([]byte(p + "\n")); err != nil {
			t.Errorf("%q should stay valid: %v", p, err)
		}
	}
}

func TestParseTermsTrailingWhitespaceIsPartOfThePattern(t *testing.T) {
	// Documented, unchanged: only CR is trimmed; a trailing space is a literal (as with grep -f).
	terms, err := ParseTerms([]byte("acmecorp \n"))
	if err != nil {
		t.Fatal(err)
	}
	if terms.Match("acmecorp") || !terms.Match("acmecorp x") {
		t.Error("the trailing space is part of the pattern")
	}
	// A line of whitespace only, and a comment indented, are ignored.
	if _, err := ParseTerms([]byte(" \t \n \t# note\n")); !errors.Is(err, ErrAbsent) {
		t.Errorf("whitespace and indented comment: %v", err)
	}
}

func TestParseTermsCommentOnlyAtLineStart(t *testing.T) {
	// "#" mid-line is a literal; only a line whose first non-blank rune is # is a comment.
	terms, err := ParseTerms([]byte("acme#corp\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("x acme#corp") || terms.Match("acme") {
		t.Error("# mid-line is literal")
	}
}

func TestParseTermsInvalidAmongValidFailsClosed(t *testing.T) {
	for _, in := range []string{"acmecorp\n[unclosed\n", "[unclosed\nacmecorp\n", "acmecorp\r\n(\r\n", "acmecorp\nx{2,1}\n"} {
		if terms, err := ParseTerms([]byte(in)); err == nil {
			t.Errorf("%q should be refused (%v)", in, terms)
		}
	}
}

func TestLoadTermsUnreadablePathFailsClosed(t *testing.T) {
	// A directory is not a list.
	if _, err := LoadTerms(t.TempDir()); err == nil {
		t.Fatal("a directory is not a list")
	}
}
