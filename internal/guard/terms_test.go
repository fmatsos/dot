package guard

import (
	"errors"
	"strings"
	"testing"
)

const fictional = "# fictional terms\nacmecorp\nglobex-?inc\n(^|[^a-z0-9])zed([^a-z0-9]|$)\n"

func TestParseTermsMatchesLikeTheBashList(t *testing.T) {
	terms, err := ParseTerms([]byte(fictional))
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range []string{"tenant AcmeCorp", "fix for Globex-Inc", "globexinc", "client zed", "zed", "notes-acmecorp.txt"} {
		if !terms.Match(hit) {
			t.Errorf("%q should match", hit)
		}
	}
	for _, miss := range []string{"items and systems, zedd", "zedd", "organized", "nothing"} {
		if terms.Match(miss) {
			t.Errorf("%q should not match", miss)
		}
	}
	// zed_tickets: "_" is not in [a-z0-9], so it is a whole-word hit, as with grep -E.
	if !terms.Match("repo: zed_tickets") {
		t.Error("zed_tickets should match")
	}
}

func TestParseTermsAccents(t *testing.T) {
	terms, err := ParseTerms([]byte("acmé\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("ACMÉ.txt") || !terms.Match("acmé.txt") || terms.Match("café.txt") {
		t.Error("case insensitivity of accented characters")
	}
}

func TestParseTermsIgnoresCommentsBlankLinesAndCRLF(t *testing.T) {
	terms, err := ParseTerms([]byte("  # note\r\n\r\n   \r\nacmecorp\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("AcmeCorp") {
		t.Error("the term followed by CRLF should match")
	}
}

func TestParseTermsFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     error
	}{
		{"empty", "", ErrAbsent},
		{"comments", "# a\n\n   \n", ErrAbsent},
		{"open parenthesis", "acmecorp\nbroken(\n", ErrInvalid},
		{"unbalanced parentheses", "(a)b)(c\n", ErrInvalid},
		{"repetition without argument", "*oops\n", ErrInvalid},
		{"ERE backreference", "(a)\\1\n", ErrInvalid},
		{"invalid UTF-8", "\xff\xfe\n", ErrInvalid},
		{"GNU alternation", "acmecorp\\|globex\n", ErrInvalid},
		{"GNU plus", "acme\\+corp\n", ErrInvalid},
		{"GNU optional", "acme\\?corp\n", ErrInvalid},
		{"GNU brace", "a\\{2\\}\n", ErrInvalid},
		{"GNU opening group", "\\(acme\\)corp\n", ErrInvalid},
		{"GNU closing group", "ok\n(acme\\)\n", ErrInvalid},
		{"case sensitivity restored", "(?-i)acmecorp\n", ErrInvalid},
		{"flags including -i", "(?s-i:acmecorp)\n", ErrInvalid},
		{"-i in a group", "ok\n(?-i:acmecorp)\n", ErrInvalid},
	} {
		terms, err := ParseTerms([]byte(tc.in))
		if !errors.Is(err, tc.want) || terms != nil {
			t.Errorf("%s: %v, %v", tc.name, terms, err)
		}
		if err != nil && strings.Contains(err.Error(), "broken") {
			t.Errorf("%s: the error must not quote the pattern", tc.name)
		}
	}
}

func TestMatchIsLineBased(t *testing.T) {
	terms, _ := ParseTerms([]byte("a[^x]b\n"))
	if terms.Match("a\nb") {
		t.Error("a pattern does not span lines")
	}
	if !terms.Match("ok\naub") {
		t.Error("a following line must be checked")
	}
}

func TestNilTermsMatchNothing(t *testing.T) {
	var terms *Terms
	if terms.Match("acmecorp") || terms.MatchLine([]byte("acmecorp")) {
		t.Error("without a list (secrets mode), nothing matches")
	}
}

func TestLoadTermsMissingFileIsAbsent(t *testing.T) {
	if _, err := LoadTerms("/nonexistent/forbidden.local"); !errors.Is(err, ErrAbsent) {
		t.Fatal(err)
	}
}

func TestParseTermsKeepsLiteralEscapesAndCaseFlags(t *testing.T) {
	// An escaped backslash then a real alternation, and flags that keep (?i), are not GNU leftovers.
	terms, err := ParseTerms([]byte("acme\\\\|globex\n(?i)initech\n(?s)umbrella\n\\.corp\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range []string{`acme\`, "GLOBEX", "Initech", "UMBRELLA", "x.corp"} {
		if !terms.Match(hit) {
			t.Errorf("%q should match", hit)
		}
	}
	// The refusal never echoes the pattern.
	_, err = ParseTerms([]byte("secretterm\\|x\n"))
	if err == nil || strings.Contains(err.Error(), "secretterm") {
		t.Fatalf("error = %v", err)
	}
}
