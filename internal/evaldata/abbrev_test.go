package evaldata

import (
	"os"
	"path/filepath"
	"runtime"
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

// repoSeedDir locates the committed seed directory by walking up from this
// source file until seed/objects/Law.jsonl is found.
func repoSeedDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("cannot locate source file")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "seed", "objects", "Law.jsonl")); err == nil {
			return filepath.Join(dir, "seed")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("committed seed dir not found")
		}
		dir = parent
	}
}

// TestBuildAbbrevMapRealSeed checks that colloquial names and abbreviations
// resolve to the substantive law, not a later amendment act or a fragment of an
// unrelated name.
func TestBuildAbbrevMapRealSeed(t *testing.T) {
	m, err := BuildAbbrevMap(repoSeedDir(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"skatteloven":            "lov/1999-03-26-14",
		"sktl":                   "lov/1999-03-26-14",
		"inkassoloven":           "lov/1988-05-13-26",
		"personopplysningsloven": "lov/2018-06-15-38",
		"gjeldsordningsloven":    "lov/1992-07-17-99",
		"arbeidsmiljøloven":      "lov/2005-06-17-62",
		"aml":                    "lov/2005-06-17-62",
		"avtaleloven":            "lov/1918-05-31-4",
	}
	for tok, want := range cases {
		if got := m.Lookup(tok); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", tok, got, want)
		}
	}
}

// writeAmendmentFixture writes a Law.jsonl where an amendment act and a
// Jan Mayen law both produce the token "skatteloven", plus the real law.
func writeAmendmentFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	obj := filepath.Join(dir, "objects")
	if err := os.MkdirAll(obj, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "" +
		`{"type":"Law","key":"lov/1999-03-26-14","properties":{"short_title":"Skatteloven – sktl","name":"Lov om skatt av formue og inntekt (skatteloven)"}}` + "\n" +
		`{"type":"Law","key":"lov/2008-12-12-99","properties":{"short_title":"Endringslov til skatteloven","name":"Lov om endringer i lov 26. mars 1999 nr. 14 om skatt av formue og inntekt (skatteloven)"}}` + "\n" +
		`{"type":"Law","key":"lov/1996-11-29-69","properties":{"short_title":"Jan Mayen-skatteloven","name":"Lov om skattlegging av personer på Jan Mayen og i Antarktis (Jan Mayen-skatteloven)"}}` + "\n"
	if err := os.WriteFile(filepath.Join(obj, "Law.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuildAbbrevMapSkipsAmendmentAct(t *testing.T) {
	m, err := BuildAbbrevMap(writeAmendmentFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Lookup("skatteloven"); got != "lov/1999-03-26-14" {
		t.Errorf("Lookup(skatteloven) = %q, want lov/1999-03-26-14 (amendment act and Jan Mayen fragment must not win)", got)
	}
	if got := m.Lookup("sktl"); got != "lov/1999-03-26-14" {
		t.Errorf("Lookup(sktl) = %q, want lov/1999-03-26-14", got)
	}
}

// TestBuildAbbrevMapDenylist verifies generic words are never registered as act
// tokens, while distinctive names and abbreviations survive.
func TestBuildAbbrevMapDenylist(t *testing.T) {
	m, err := BuildAbbrevMap(repoSeedDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"loven", "avtalen", "m.v"} {
		if got := m.Lookup(tok); got != "" {
			t.Errorf("Lookup(%q) = %q, want empty (generic word must not map)", tok, got)
		}
	}
	if got := m.Lookup("forretningshemmelighetsloven"); got == "" {
		t.Error("Lookup(forretningshemmelighetsloven) = empty, want a key")
	}
	if got := m.Lookup("aml"); got != "lov/2005-06-17-62" {
		t.Errorf("Lookup(aml) = %q, want lov/2005-06-17-62", got)
	}
	if got := m.Lookup("sktl"); got != "lov/1999-03-26-14" {
		t.Errorf("Lookup(sktl) = %q, want lov/1999-03-26-14", got)
	}
}
