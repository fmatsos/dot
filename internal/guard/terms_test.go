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
			t.Errorf("%q devrait correspondre", hit)
		}
	}
	for _, miss := range []string{"items and systems, zedd", "zedd", "organized", "rien"} {
		if terms.Match(miss) {
			t.Errorf("%q ne devrait pas correspondre", miss)
		}
	}
	// zed_tickets: "_" is not in [a-z0-9], so it is a whole-word hit, as with grep -E.
	if !terms.Match("repo: zed_tickets") {
		t.Error("zed_tickets devrait correspondre")
	}
}

func TestParseTermsAccents(t *testing.T) {
	terms, err := ParseTerms([]byte("acmé\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("ACMÉ.txt") || !terms.Match("acmé.txt") || terms.Match("café.txt") {
		t.Error("insensibilité à la casse des caractères accentués")
	}
}

func TestParseTermsIgnoresCommentsBlankLinesAndCRLF(t *testing.T) {
	terms, err := ParseTerms([]byte("  # note\r\n\r\n   \r\nacmecorp\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !terms.Match("AcmeCorp") {
		t.Error("le terme suivi de CRLF devrait correspondre")
	}
}

func TestParseTermsFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     error
	}{
		{"vide", "", ErrAbsent},
		{"commentaires", "# a\n\n   \n", ErrAbsent},
		{"parenthèse ouverte", "acmecorp\nbroken(\n", ErrInvalid},
		{"parenthèses déséquilibrées", "(a)b)(c\n", ErrInvalid},
		{"répétition sans argument", "*oops\n", ErrInvalid},
		{"référence arrière ERE", "(a)\\1\n", ErrInvalid},
		{"UTF-8 invalide", "\xff\xfe\n", ErrInvalid},
	} {
		terms, err := ParseTerms([]byte(tc.in))
		if !errors.Is(err, tc.want) || terms != nil {
			t.Errorf("%s : %v, %v", tc.name, terms, err)
		}
		if err != nil && strings.Contains(err.Error(), "broken") {
			t.Errorf("%s : l'erreur ne doit pas citer l'expression", tc.name)
		}
	}
}

func TestMatchIsLineBased(t *testing.T) {
	terms, _ := ParseTerms([]byte("a[^x]b\n"))
	if terms.Match("a\nb") {
		t.Error("un motif ne traverse pas les lignes")
	}
	if !terms.Match("ok\naub") {
		t.Error("une ligne suivante doit être contrôlée")
	}
}

func TestNilTermsMatchNothing(t *testing.T) {
	var terms *Terms
	if terms.Match("acmecorp") || terms.MatchLine([]byte("acmecorp")) {
		t.Error("sans liste (mode secrets), rien ne correspond")
	}
}

func TestLoadTermsMissingFileIsAbsent(t *testing.T) {
	if _, err := LoadTerms("/nonexistent/forbidden.local"); !errors.Is(err, ErrAbsent) {
		t.Fatal(err)
	}
}
