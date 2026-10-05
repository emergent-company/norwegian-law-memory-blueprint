package evaldata

import (
	"os"
	"path/filepath"
	"testing"
)

// writeLawFixture writes a minimal Law.jsonl for abbreviation-map tests.
func writeLawFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	obj := filepath.Join(dir, "objects")
	if err := os.MkdirAll(obj, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "" +
		`{"type":"Law","key":"lov/2002-06-21-34","properties":{"short_title":"Forbrukerkjøpsloven – fkjl","name":"Lov om forbrukerkjøp (forbrukerkjøpsloven)"}}` + "\n" +
		`{"type":"Law","key":"lov/2005-05-20-28","properties":{"short_title":"Straffeloven – strl.","name":"Lov om straff (straffeloven)"}}` + "\n" +
		`{"type":"Law","key":"lov/1967-02-10","properties":{"short_title":"Forvaltningsloven – fvl","name":"Lov om behandlingsmåten i forvaltningssaker (forvaltningsloven)"}}` + "\n" +
		`{"type":"Regulation","key":"forskrift/2006-02-17-204","properties":{"short_title":"En forskrift","name":"Forskrift om noe"}}` + "\n"
	if err := os.WriteFile(filepath.Join(obj, "Law.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuildAbbrevMap(t *testing.T) {
	m, err := BuildAbbrevMap(writeLawFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"fkjl":                "lov/2002-06-21-34",
		"forbrukerkjøpsloven": "lov/2002-06-21-34",
		"strl":                "lov/2005-05-20-28",
		"strl.":               "lov/2005-05-20-28",
		"straffeloven":        "lov/2005-05-20-28",
		"fvl":                 "lov/1967-02-10",
		"forvaltningsloven":   "lov/1967-02-10",
	}
	for tok, want := range cases {
		if got := m.Lookup(tok); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", tok, got, want)
		}
	}
	if got := m.Lookup("ukjent"); got != "" {
		t.Errorf("Lookup(ukjent) = %q, want empty", got)
	}
}

func TestNormalizeActToken(t *testing.T) {
	cases := map[string]string{
		"fkjl":  "fkjl",
		"strl.": "strl",
		"Fkjl":  "fkjl",
		" skl ": "skl",
	}
	for in, want := range cases {
		if got := NormalizeActToken(in); got != want {
			t.Errorf("NormalizeActToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildAbbrevMapMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := BuildAbbrevMap(dir); err == nil {
		t.Fatal("expected error for missing Law.jsonl")
	}
}
