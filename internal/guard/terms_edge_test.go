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
		t.Error("le premier terme d'une liste avec BOM doit correspondre")
	}
	// A BOM then a comment, alone: still an empty list, refused.
	if _, err := ParseTerms([]byte("\xef\xbb\xbf# note\n")); !errors.Is(err, ErrAbsent) {
		t.Errorf("BOM + commentaire : %v", err)
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
				t.Errorf("%q : l'erreur cite l'expression", in)
			}
		}
	}
	// Non-empty-matching patterns that look similar stay valid.
	for _, p := range []string{"a+", "a*b", "^a", "a$", "(acme)?corp"} {
		if _, err := ParseTerms([]byte(p + "\n")); err != nil {
			t.Errorf("%q devrait rester valide : %v", p, err)
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
		t.Error("l'espace final fait partie du motif")
	}
	// A line of whitespace only, and a comment indented, are ignored.
	if _, err := ParseTerms([]byte(" \t \n \t# note\n")); !errors.Is(err, ErrAbsent) {
		t.Errorf("espaces et commentaire indenté : %v", err)
	}
}

func TestParseTermsCommentOnlyAtLineStart(t *testing.T) {
	// "#" mid-line is a literal; only a line whose first non-blank rune is # is a comment.
	terms, err := ParseTerms([]byte("acme#corp\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("x acme#corp") || terms.Match("acme") {
		t.Error("# au milieu d'une ligne est littéral")
	}
}

func TestParseTermsInvalidAmongValidFailsClosed(t *testing.T) {
	for _, in := range []string{"acmecorp\n[unclosed\n", "[unclosed\nacmecorp\n", "acmecorp\r\n(\r\n", "acmecorp\nx{2,1}\n"} {
		if terms, err := ParseTerms([]byte(in)); err == nil {
			t.Errorf("%q devrait être refusé (%v)", in, terms)
		}
	}
}

func TestLoadTermsUnreadablePathFailsClosed(t *testing.T) {
	// A directory is not a list.
	if _, err := LoadTerms(t.TempDir()); err == nil {
		t.Fatal("un dossier n'est pas une liste")
	}
}
