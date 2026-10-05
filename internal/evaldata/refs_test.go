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
		"fkjl":                "lov/2002-06-21-34",
		"forbrukerkjøpsloven": "lov/2002-06-21-34",
		"strl":                "lov/2005-05-20-28",
		"straffeloven":        "lov/2005-05-20-28",
		"avtl":                "lov/1918-05-31-4",
		"avtaleloven":         "lov/1918-05-31-4",
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

func TestExtractRefsCarryForwardAcrossLines(t *testing.T) {
	text := "Vurder forholdet etter avtaleloven.\n\nHer gjelder § 36.\n\nOg § 33."
	got := ExtractRefs(text, testAbbrevs())
	want := []RefCandidate{
		{ActKey: "lov/1918-05-31-4", Section: "36", Raw: "§ 36"},
		{ActKey: "lov/1918-05-31-4", Section: "33", Raw: "§ 33"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtractRefsNoAttribution(t *testing.T) {
	text := "Se § 16 uten å nevne noen lov."
	got := ExtractRefs(text, testAbbrevs())
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
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
