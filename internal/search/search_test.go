package search

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestQueryQuotesWordsAndPrefixesTheLast(t *testing.T) {
	for in, want := range map[string]string{
		"lexer":               `("lexer" OR "lexer"*)`,
		"Parser LEX":          `"parser" AND ("lex" OR "lex"*)`,
		`c++ "NEAR" foo-bar*`: `"c" AND "near" AND "foo" AND ("bar" OR "bar"*)`,
		"don't":               `("don't" OR "don't"*)`,
		"  !!! ":              "",
	} {
		if got := Query(in); got != want {
			t.Errorf("Query(%q) = %s, want %s", in, got, want)
		}
	}
}

// Splitting a contraction would make it unfindable by either half.
func TestApostrophesStayInsideWords(t *testing.T) {
	got := Tokenize("don't stop 'quoted' word")
	want := []string{"don't", "stop", "quoted", "word"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Tokenize = %v, want %v", got, want)
	}
}

func TestTokenizeEmptyAndSymbolOnly(t *testing.T) {
	for _, in := range []string{"", "   ", "!!! ---", "\n\t"} {
		if got := Tokenize(in); len(got) != 0 {
			t.Errorf("Tokenize(%q) = %v, want nothing", in, got)
		}
	}
}

// FTS5 keeps a combining mark inside its word, so a decomposed (NFD) query
// must too, or "résumé" becomes the terms "re" and "sume".
func TestCombiningMarksStayInsideWords(t *testing.T) {
	got := Tokenize("Résumé ́x")
	want := []string{"résumé", "x"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Tokenize = %q, want %q", got, want)
	}
}

// Queries in either normal form find text in either, under the tokenizer the
// wiki cache uses.
func TestQueryMatchesFTS5AcrossNormalForms(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIRTUAL TABLE f USING fts5 (body, tokenize = 'unicode61 remove_diacritics 2')`); err != nil {
		t.Fatal(err)
	}
	nfc, nfd := "résumé naïve", "résumé naïve"
	for _, text := range []string{nfc, nfd} {
		if _, err := db.Exec(`INSERT INTO f (body) VALUES (?)`, text); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{nfc, nfd, "résu"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM f WHERE f MATCH ?`, Query(q)).Scan(&n); err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if n != 2 {
			t.Errorf("Query(%q) = %s matches %d rows, want 2", q, Query(q), n)
		}
	}
}
