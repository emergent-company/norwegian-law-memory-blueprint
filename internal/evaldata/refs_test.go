package evaldata

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
)

// writeRefSeed writes a minimal seed for reference-resolution tests, covering
// one act (forbrukerkjøpsloven) with two sections.
func writeRefSeed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	obj := filepath.Join(dir, "objects")
	rel := filepath.Join(dir, "relationships")
	if err := os.MkdirAll(obj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	law := "" +
		`{"type":"Law","key":"lov/2002-06-21-34","properties":{"short_title":"Forbrukerkjøpsloven – fkjl","name":"Lov om forbrukerkjøp","content":"### § 16 — Reklamasjon\n\n### § 17 — Oversikt\n"}}` + "\n"
	paras := "" +
		`{"type":"LegalParagraph","key":"lov/2002-06-21-34#kapittel-1-paragraf-16","properties":{"law_ref_id":"lov/2002-06-21-34","paragraph_num":"§ 16","section_id":"kapittel-1-paragraf-16","content":"Reklamasjon."}}` + "\n" +
		`{"type":"LegalParagraph","key":"lov/2002-06-21-34#kapittel-1-paragraf-17","properties":{"law_ref_id":"lov/2002-06-21-34","paragraph_num":"§ 17","section_id":"kapittel-1-paragraf-17","content":"Oversikt."}}` + "\n"
	rels := "" +
		`{"type":"HAS_PARAGRAPH","srcKey":"lov/2002-06-21-34","dstKey":"lov/2002-06-21-34#kapittel-1-paragraf-16","properties":{"position":1}}` + "\n" +
		`{"type":"HAS_PARAGRAPH","srcKey":"lov/2002-06-21-34","dstKey":"lov/2002-06-21-34#kapittel-1-paragraf-17","properties":{"position":2}}` + "\n"
	if err := os.WriteFile(filepath.Join(obj, "Law.jsonl"), []byte(law), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(obj, "LegalParagraph.jsonl"), []byte(paras), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rel, "HAS_PARAGRAPH.jsonl"), []byte(rels), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testAbbrevs() AbbrevMap {
	return AbbrevMap{
		"fkjl":                         "lov/2002-06-21-34",
		"forbrukerkjøpsloven":          "lov/2002-06-21-34",
		"strl":                         "lov/2005-05-20-28",
		"straffeloven":                 "lov/2005-05-20-28",
		"avtl":                         "lov/1918-05-31-4",
		"avtaleloven":                  "lov/1918-05-31-4",
		"forretningshemmelighetsloven": "lov/2020-03-27-15",
	}
}

func TestExtractRefsAbbreviation(t *testing.T) {
	text := "Etter fkjl § 16 kan forbrukeren reklamere. Se også § 17."
	got := ExtractRefs(text, testAbbrevs())
	want := []RefCandidate{
		{ActKey: "lov/2002-06-21-34", Section: "16", Raw: "§ 16"},
		{ActKey: "lov/2002-06-21-34", Section: "17", Raw: "§ 17"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtractRefsTrailingDotAndFullName(t *testing.T) {
	text := "Jf. fkjl. § 16. Straffeloven § 20 kan også komme til anvendelse."
	got := ExtractRefs(text, testAbbrevs())
	want := []RefCandidate{
		{ActKey: "lov/2002-06-21-34", Section: "16", Raw: "§ 16"},
		{ActKey: "lov/2005-05-20-28", Section: "20", Raw: "§ 20"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestExtractRefsCarryForwardAcrossLines checks the bounded cross-line carry: an
// act heading line (one act, no §) carries its act to the immediately following
// § line, but a § line resets the heading so later unattributed § tokens drop.
func TestExtractRefsCarryForwardAcrossLines(t *testing.T) {
	text := "Forretningshemmelighetsloven\n\n§ 2 gir grunnlag."
	got := ExtractRefs(text, testAbbrevs())
	want := []RefCandidate{
		{ActKey: "lov/2020-03-27-15", Section: "2", Raw: "§ 2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestExtractRefsNoUnboundedCarry verifies that an act named on a distant line
// does not absorb § references separated by an intervening non-heading line.
func TestExtractRefsNoUnboundedCarry(t *testing.T) {
	text := "Avtaleloven er utgangspunktet.\n\nEn mellomliggende setning uten lovhenvisning.\n\n§ 36 må vurderes."
	got := ExtractRefs(text, testAbbrevs())
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty (unbounded carry removed)", got)
	}
}

func TestExtractRefsNoAttribution(t *testing.T) {
	text := "Se § 16 uten å nevne noen lov."
	got := ExtractRefs(text, testAbbrevs())
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

func TestExtractRefsExplicitActKey(t *testing.T) {
	text := "Skatteloven (lov/1999-03-26-14) § 6-1 gir grunnlag."
	got := ExtractRefs(text, testAbbrevs())
	want := []RefCandidate{
		{ActKey: "lov/1999-03-26-14", Section: "6-1", Raw: "§ 6-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtractRefsExplicitActKeyInLink(t *testing.T) {
	text := "Se [forskriften](/objects/forskrift/2006-02-17-204) § 2."
	got := ExtractRefs(text, testAbbrevs())
	want := []RefCandidate{
		{ActKey: "forskrift/2006-02-17-204", Section: "2", Raw: "§ 2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestExtractRefsGenericWordNotAttributed verifies that generic words like
// "avtalen"/"loven" are not act tokens, so a § following them is not attributed
// to a law whose name merely contained the word (e.g. CFE-avtalen).
func TestExtractRefsGenericWordNotAttributed(t *testing.T) {
	dir := t.TempDir()
	obj := filepath.Join(dir, "objects")
	if err := os.MkdirAll(obj, 0o755); err != nil {
		t.Fatal(err)
	}
	law := `{"type":"Law","key":"lov/1992-05-29-50","properties":{"short_title":"CFE-avtalen","name":"Lov om inspeksjoner i samsvar med Avtalen om konvensjonelle styrker i Europa (CFE-avtalen)"}}` + "\n"
	if err := os.WriteFile(filepath.Join(obj, "Law.jsonl"), []byte(law), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := BuildAbbrevMap(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := ExtractRefs("avtalen § 2 og loven § 3.", m)
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty (generic words must not attribute §)", got)
	}
}

func TestResolveCandidates(t *testing.T) {
	dir := writeRefSeed(t)
	idx, err := lovcite.Build(dir, lovcite.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cands := []RefCandidate{
		{ActKey: "lov/2002-06-21-34", Section: "16", Raw: "§ 16"},
		{ActKey: "lov/2002-06-21-34", Section: "17", Raw: "§ 17"},
		{ActKey: "lov/2002-06-21-34", Section: "99", Raw: "§ 99"}, // section missing
		{ActKey: "lov/1999-99-99-9", Section: "1", Raw: "§ 1"},    // act missing
	}
	resolved, unresolved := ResolveCandidates(cands, idx)
	wantResolved := []string{"lov/2002-06-21-34#§16", "lov/2002-06-21-34#§17"}
	if !reflect.DeepEqual(resolved, wantResolved) {
		t.Errorf("resolved = %v, want %v", resolved, wantResolved)
	}
	if len(unresolved) != 2 {
		t.Fatalf("unresolved = %d, want 2: %+v", len(unresolved), unresolved)
	}
}

func TestResolveCandidatesDedupSorted(t *testing.T) {
	dir := writeRefSeed(t)
	idx, err := lovcite.Build(dir, lovcite.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cands := []RefCandidate{
		{ActKey: "lov/2002-06-21-34", Section: "17", Raw: "§ 17"},
		{ActKey: "lov/2002-06-21-34", Section: "16", Raw: "§ 16"},
		{ActKey: "lov/2002-06-21-34", Section: "16", Raw: "§ 16"},
	}
	resolved, _ := ResolveCandidates(cands, idx)
	want := []string{"lov/2002-06-21-34#§16", "lov/2002-06-21-34#§17"}
	if !reflect.DeepEqual(resolved, want) {
		t.Errorf("resolved = %v, want %v", resolved, want)
	}
}
