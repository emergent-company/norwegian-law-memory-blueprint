package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legalPOnlyFixture reproduces the top-level-legalP structure of
// /tmp/lov_raw/del/sf-20240708-1622.xml: a short delegation regulation whose
// body text lives in <article class="legalP"> directly under <section>, with no
// enclosing <article class="legalArticle">.
const legalPOnlyFixture = `<!DOCTYPE html><html lang="nb"><head><title>Delegering av myndighet til Vegdirektoratet etter vegtrafikkloven § 23 tredje ledd</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-07-08-1622</dd>
<dt class="dateInForce">I kraft fra</dt><dd class="dateInForce">2024-07-08</dd>
<dt class="dateOfPublication">Kunngjort</dt><dd class="dateOfPublication">2024-07-09 14:55</dd>
<dt class="title">Tittel</dt><dd class="title">Delegering av myndighet til Vegdirektoratet etter vegtrafikkloven § 23 tredje ledd</dd>
</dl></header>
<main class="documentBody" id="dokument">
<h1>Delegering av myndighet til Vegdirektoratet etter vegtrafikkloven § 23 tredje ledd</h1>
<section class="section" id="kapittel-1"><h2 data-text-align="center">I</h2><article class="legalP" id="kapittel-1-ledd-1">Samferdselsdepartementets myndighet etter <a href="lov/1965-06-18-4/§23/ledd/3">lov 18. juni 1965 nr. 4 om vegtrafikk § 23 tredje ledd</a> delegeres til Vegdirektoratet.</article></section>
<section class="section" id="kapittel-2"><h2 data-text-align="center">II</h2><article class="legalP" id="kapittel-2-ledd-1">Vedtaket trer i kraft straks.</article></section>
</main></body></html>`

func TestParseLegalPOnlyDocument(t *testing.T) {
	doc := parseDocument([]byte(legalPOnlyFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}
	if doc.RefID != "forskrift/2024-07-08-1622" {
		t.Fatalf("RefID = %q", doc.RefID)
	}

	const wantContent = "Samferdselsdepartementets myndighet etter lov 18. juni 1965 nr. 4 om vegtrafikk § 23 tredje ledd delegeres til Vegdirektoratet.\n\nVedtaket trer i kraft straks."
	if doc.Content != wantContent {
		t.Fatalf("Content mismatch:\n got: %q\nwant: %q", doc.Content, wantContent)
	}
	if strings.Contains(doc.Content, "Kapittel") {
		t.Fatalf("content must not contain bogus roman-only chapter headings: %q", doc.Content)
	}

	if len(doc.Paragraphs) != 2 {
		t.Fatalf("Paragraphs len = %d, want 2: %+v", len(doc.Paragraphs), doc.Paragraphs)
	}
	p0 := doc.Paragraphs[0]
	if p0.SectionID != "kapittel-1-ledd-1" || p0.ChapterID != "kapittel-1" {
		t.Fatalf("paragraph[0] = %+v", p0)
	}
	if !strings.Contains(p0.Content, "delegeres til Vegdirektoratet.") {
		t.Fatalf("paragraph[0].Content = %q", p0.Content)
	}
	p1 := doc.Paragraphs[1]
	if p1.SectionID != "kapittel-2-ledd-1" || p1.ChapterID != "kapittel-2" {
		t.Fatalf("paragraph[1] = %+v", p1)
	}

	foundRef := false
	for _, ref := range doc.References {
		if ref == "lov/1965-06-18-4/§23/ledd/3" {
			foundRef = true
		}
	}
	if !foundRef {
		t.Fatalf("cross-ref not collected; References = %v", doc.References)
	}
}

// legalArticleFixture reproduces the standard § (article) structure of the bulk
// Lovdata corpus: an <article class="legalArticle"> whose body text lives in
// nested <article class="legalP"> children under a legalArticleHeader.
const legalArticleFixture = `<!DOCTYPE html><html lang="nb"><head><title>Lov om arbeidsmiljø</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">lov/2005-06-17-90</dd>
<dt class="title">Tittel</dt><dd class="title">Lov om arbeidsmiljø</dd>
</dl></header>
<main class="documentBody" id="dokument">
<h1>Lov om arbeidsmiljø</h1>
<article class="legalArticle" id="paragraf-1"><h3 class="legalArticleHeader"><span class="legalArticleValue">§ 1</span>. <span class="legalArticleTitle">Formål</span></h3><article class="legalP" id="paragraf-1-ledd-1">Lovens formål er å sikre trygge arbeidsforhold.</article><article class="legalP" id="paragraf-1-ledd-2">Arbeidsgiver skal sørge for et fullt forsvarlig arbeidsmiljø.</article></article>
</main></body></html>`

func TestParseLegalArticleDocument(t *testing.T) {
	doc := parseDocument([]byte(legalArticleFixture), "Law")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}

	const wantContent = "### § 1 — Formål\n\nLovens formål er å sikre trygge arbeidsforhold.\n\nArbeidsgiver skal sørge for et fullt forsvarlig arbeidsmiljø."
	if doc.Content != wantContent {
		t.Fatalf("Content mismatch:\n got: %q\nwant: %q", doc.Content, wantContent)
	}

	if len(doc.Paragraphs) != 1 {
		t.Fatalf("Paragraphs len = %d, want 1: %+v", len(doc.Paragraphs), doc.Paragraphs)
	}
	p := doc.Paragraphs[0]
	if p.SectionID != "paragraf-1" || p.ParagraphNum != "§ 1" || p.Title != "Formål" {
		t.Fatalf("paragraph = %+v", p)
	}
	if !strings.Contains(p.Content, "Lovens formål er å sikre trygge arbeidsforhold.") ||
		!strings.Contains(p.Content, "Arbeidsgiver skal sørge for et fullt forsvarlig arbeidsmiljø.") {
		t.Fatalf("paragraph content missing body text: %q", p.Content)
	}
}

// mixedDocFixture places a top-level legalP AND a legalArticle in the same
// <main>, guarding against double-capture of the nested legalP paragraphs.
const mixedDocFixture = `<!DOCTYPE html><html lang="nb"><head><title>Mixed</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-1</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>I</h2><article class="legalP" id="kapittel-1-ledd-1">Top-level paragraph text.</article></section>
<article class="legalArticle" id="paragraf-1"><h3 class="legalArticleHeader"><span class="legalArticleValue">§ 1</span>. <span class="legalArticleTitle">Bestemmelse</span></h3><article class="legalP" id="paragraf-1-ledd-1">Nested article paragraph.</article></article>
</main></body></html>`

func TestParseMixedLegalPAndArticle(t *testing.T) {
	doc := parseDocument([]byte(mixedDocFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}

	if n := strings.Count(doc.Content, "Top-level paragraph text."); n != 1 {
		t.Fatalf("top-level legalP text count = %d, want 1: %q", n, doc.Content)
	}
	if n := strings.Count(doc.Content, "Nested article paragraph."); n != 1 {
		t.Fatalf("nested legalP text count = %d, want 1 (double-captured?): %q", n, doc.Content)
	}

	if len(doc.Paragraphs) != 2 {
		t.Fatalf("Paragraphs len = %d, want 2: %+v", len(doc.Paragraphs), doc.Paragraphs)
	}
}

// romanPeriodFixture uses an <h2> with a trailing period ("I."), which must also
// be suppressed as a roman-only section divider.
const romanPeriodFixture = `<!DOCTYPE html><html lang="nb"><head><title>X</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-1</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>I.</h2><article class="legalP" id="kapittel-1-ledd-1">Some body text.</article></section>
</main></body></html>`

func TestRomanOnlyHeadingWithPeriodSuppressed(t *testing.T) {
	doc := parseDocument([]byte(romanPeriodFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}
	if strings.Contains(doc.Content, "Kapittel") {
		t.Fatalf("content must not contain a roman-only chapter heading with trailing period: %q", doc.Content)
	}
	if !strings.Contains(doc.Content, "Some body text.") {
		t.Fatalf("body text lost: %q", doc.Content)
	}
}

// dotHeadingFixture uses a lone "." as the <h2>. It is not a Roman numeral, so
// it must NOT be swallowed by the roman-only divider suppression.
const dotHeadingFixture = `<!DOCTYPE html><html lang="nb"><head><title>X</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-2</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>.</h2><article class="legalArticle" id="kapittel-1-paragraf-1"><h3 class="legalArticleHeader"><span class="legalArticleValue">§ 1</span><span class="legalArticleTitle">Formål</span></h3><article class="legalP">Body.</article></article></section>
</main></body></html>`

func TestLonePeriodHeadingNotSuppressedAsRoman(t *testing.T) {
	doc := parseDocument([]byte(dotHeadingFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}
	if !strings.Contains(doc.Content, "Kapittel") {
		t.Fatalf("lone '.' heading must not be suppressed as roman-only: %q", doc.Content)
	}
}

func TestNormalizeDateValue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
		ok   bool
	}{
		{"iso date", "2024-07-08", "2024-07-08", true},
		{"clock time no seconds", "2024-07-09 14:55", "2024-07-09", true},
		{"clock time with seconds", "2024-07-09 14:55:30", "2024-07-09", true},
		{"dot date", "02.01.2006", "2006-01-02", true},
		{"dot date with time", "02.01.2006 15:04", "2006-01-02", true},
		{"free text in-force year", "Inntektsåret 1998", "", false},
		{"multi date list", "1965-07-01, 1967-04-23", "", false},
		{"deferred free text", "Kongen bestemmer, 2004-01-01, 2005-01-01", "", false},
		{"year beyond 2050 typo", "2051-01-01", "", false},
		{"empty", "", "", false},
		{"non-string", 123, "", false},
		{"nil", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeDateValue(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("normalizeDateValue(%v) = (%q, %v); want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestNormalizeDateOfPublicationDateOnly(t *testing.T) {
	props := map[string]any{
		"date_of_publication": "2024-07-09 14:55",
	}
	normCounts := map[string]int{}
	rawMoved := 0
	normalizeDateProps("Regulation", props, normCounts, &rawMoved)

	if got := props["date_of_publication"]; got != "2024-07-09" {
		t.Fatalf("date_of_publication = %v, want 2024-07-09", got)
	}
	if _, ok := props["date_of_publication_raw"]; ok {
		t.Fatalf("date_of_publication_raw should not be set for a parseable value: %v", props)
	}
	if rawMoved != 0 {
		t.Fatalf("rawMoved = %d, want 0", rawMoved)
	}
}

func TestNormalizeDatePropsRawFallback(t *testing.T) {
	props := map[string]any{
		"date_in_force":        "Inntektsåret 1998",
		"date_of_publication":  "some unparseable text",
		"last_change_in_force": "2024-01-01",
	}
	normCounts := map[string]int{}
	rawMoved := 0
	normalizeDateProps("Regulation", props, normCounts, &rawMoved)

	if _, ok := props["date_in_force"]; ok {
		t.Fatalf("date_in_force should be dropped")
	}
	if props["date_in_force_raw"] != "Inntektsåret 1998" {
		t.Fatalf("date_in_force_raw = %v", props["date_in_force_raw"])
	}
	if _, ok := props["date_of_publication"]; ok {
		t.Fatalf("date_of_publication should be dropped")
	}
	if props["date_of_publication_raw"] != "some unparseable text" {
		t.Fatalf("date_of_publication_raw = %v", props["date_of_publication_raw"])
	}
	if props["last_change_in_force"] != "2024-01-01" {
		t.Fatalf("last_change_in_force = %v", props["last_change_in_force"])
	}
	if rawMoved != 2 {
		t.Fatalf("rawMoved = %d, want 2", rawMoved)
	}
}

func TestSplitJSONLWriter(t *testing.T) {
	dir := t.TempDir()

	// Small split threshold to exercise the rename path without writing 50 MB.
	old := dumpSplitSize
	dumpSplitSize = 64
	defer func() { dumpSplitSize = old }()

	// Build records large enough to force 3 files.
	var recs []seedObjectRecord
	for i := 0; i < 10; i++ {
		recs = append(recs, seedObjectRecord{
			Type: "LegalParagraph",
			Key:  fmt.Sprintf("k%d", i),
			Properties: map[string]any{
				"content": strings.Repeat("x", 64),
			},
		})
	}

	if err := writeObjectJSONL(dir, "LegalParagraph", recs); err != nil {
		t.Fatalf("writeObjectJSONL: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	totalLines := 0
	for _, e := range entries {
		names = append(names, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line != "" {
				totalLines++
			}
		}
	}
	if totalLines != len(recs) {
		t.Fatalf("total lines across split files = %d, want %d", totalLines, len(recs))
	}
	if len(names) < 2 {
		t.Fatalf("expected multiple split files, got %v", names)
	}
	// First split file must use the .001.jsonl naming convention.
	found001 := false
	for _, n := range names {
		if strings.HasSuffix(n, ".001.jsonl") {
			found001 = true
		}
	}
	if !found001 {
		t.Fatalf("no .001.jsonl split file among %v", names)
	}
}

// nestedSectionFixture has an inner <section> nested inside an outer <section>,
// with a legalP after the inner section closes. The outer paragraph must carry
// the outer chapter id (the inner id must not leak out).
const nestedSectionFixture = `<!DOCTYPE html><html lang="nb"><head><title>Nested</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-3</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>Kapittel 1</h2><section class="section" id="kapittel-1-del-1"><h2>Del 1</h2><article class="legalP" id="kapittel-1-del-1-ledd-1">Inner paragraph text.</article></section><article class="legalP" id="kapittel-1-ledd-1">Outer paragraph after inner section.</article></section>
</main></body></html>`

func TestNestedSectionChapterIDRestored(t *testing.T) {
	doc := parseDocument([]byte(nestedSectionFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}
	if len(doc.Paragraphs) != 2 {
		t.Fatalf("Paragraphs len = %d, want 2: %+v", len(doc.Paragraphs), doc.Paragraphs)
	}
	inner, outer := doc.Paragraphs[0], doc.Paragraphs[1]
	if inner.SectionID != "kapittel-1-del-1-ledd-1" || inner.ChapterID != "kapittel-1-del-1" {
		t.Fatalf("inner paragraph = %+v; want ChapterID kapittel-1-del-1", inner)
	}
	if outer.SectionID != "kapittel-1-ledd-1" || outer.ChapterID != "kapittel-1" {
		t.Fatalf("outer paragraph = %+v; want ChapterID kapittel-1 (inner id leaked?)", outer)
	}
}

func TestYearFromLooseDate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
		ok   bool
	}{
		{"iso date", "2004-01-01", 2004, true},
		{"free text year", "Inntektsåret 1998", 1998, true},
		{"first of several", "Kongen bestemmer, 2004-01-01, 2005-01-01", 2004, true},
		{"no year", "Ikkje i kraft", 0, false},
		{"empty", "", 0, false},
		{"pre-1900", "1814-05-17", 1814, true},
		{"beyond 2050", "abc 3000-01-01", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := yearFromLooseDate(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("yearFromLooseDate(%q) = (%d, %v); want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSortDirectives(t *testing.T) {
	dirs := []*EUDirective{
		{DirectiveID: "2010/75/EU", CelexID: "32010L0075"},
		{DirectiveID: "", CelexID: "31985L0337"}, // fallback to CelexID
		{DirectiveID: "2004/35/CE", CelexID: "32004L0035"},
		{DirectiveID: "1999/31/EC", CelexID: "31999L0031"},
	}
	sortDirectives(dirs)
	want := []string{"1999/31/EC", "2004/35/CE", "2010/75/EU", "31985L0337"}
	for i, d := range dirs {
		got := d.DirectiveID
		if got == "" {
			got = d.CelexID
		}
		if got != want[i] {
			t.Fatalf("sortDirectives order[%d] = %q; want %q (full: %+v)", i, got, want[i], dirs)
		}
	}
}

func TestSortConcepts(t *testing.T) {
	concepts := []*EuroVocConcept{
		{ID: "200", LabelEN: "b"},
		{ID: "10", LabelEN: "a"},
		{ID: "100", LabelEN: "c"},
		{ID: "20", LabelEN: "d"},
	}
	sortConcepts(concepts)
	want := []string{"10", "100", "20", "200"}
	for i, c := range concepts {
		if c.ID != want[i] {
			t.Fatalf("sortConcepts order[%d] = %q; want %q", i, c.ID, want[i])
		}
	}
}

// chapterTitleFixture uses an <h2> that already carries the "Kapittel" prefix.
const chapterTitleFixture = `<!DOCTYPE html><html lang="nb"><head><title>X</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-4</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>Kapittel 1. Innledende bestemmelser</h2><article class="legalP" id="kapittel-1-ledd-1">Body text.</article></section>
</main></body></html>`

func TestChapterTitlePrefixNotDuplicated(t *testing.T) {
	doc := parseDocument([]byte(chapterTitleFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}
	if n := strings.Count(doc.Content, "Kapittel"); n != 1 {
		t.Fatalf("content contains %d occurrences of 'Kapittel', want exactly 1: %q", n, doc.Content)
	}
	if !strings.Contains(doc.Content, "## Kapittel 1. Innledende bestemmelser") {
		t.Fatalf("heading not emitted as-is: %q", doc.Content)
	}
}

// partHeadingFixture uses an <h2> that labels a part ("Del I."), which must be
// kept as authored, not prefixed with "Kapittel".
const partHeadingFixture = `<!DOCTYPE html><html lang="nb"><head><title>X</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-5</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>Del I. Innledende bestemmelser</h2><article class="legalP" id="kapittel-1-ledd-1">Body text.</article></section>
</main></body></html>`

func TestPartHeadingKeptAsAuthored(t *testing.T) {
	doc := parseDocument([]byte(partHeadingFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}
	if strings.Contains(doc.Content, "## Kapittel") {
		t.Fatalf("part heading must not be prefixed with 'Kapittel': %q", doc.Content)
	}
	if !strings.Contains(doc.Content, "## Del I. Innledende bestemmelser") {
		t.Fatalf("part heading not emitted as-authored: %q", doc.Content)
	}
}

// normalTitleFixture uses a plain chapter title that must still be prefixed.
const normalTitleFixture = `<!DOCTYPE html><html lang="nb"><head><title>X</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-6</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>Virkeområde</h2><article class="legalP" id="kapittel-1-ledd-1">Body text.</article></section>
</main></body></html>`

func TestNormalChapterHeadingPrefixed(t *testing.T) {
	doc := parseDocument([]byte(normalTitleFixture), "Regulation")
	if doc == nil {
		t.Fatal("parseDocument returned nil")
	}
	if !strings.Contains(doc.Content, "## Kapittel 1. Virkeområde") {
		t.Fatalf("normal title not prefixed correctly: %q", doc.Content)
	}
}

// headingDoc builds a minimal regulation document whose single <section
// id="kapittel-1"> carries the given <h2> title, so the emitted markdown
// heading can be asserted independently.
func headingDoc(title string) string {
	return fmt.Sprintf(`<!DOCTYPE html><html lang="nb"><head><title>X</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">forskrift/2024-01-01-9</dd>
</dl></header>
<main class="documentBody" id="dokument">
<section class="section" id="kapittel-1"><h2>%s</h2><article class="legalP" id="kapittel-1-ledd-1">Body text.</article></section>
</main></body></html>`, title)
}

// assertHeading parses headingDoc(title) and checks the emitted markdown
// contains wantSub and does not contain wantNotSub.
func assertHeading(t *testing.T, title, wantSub, wantNotSub string) {
	t.Helper()
	doc := parseDocument([]byte(headingDoc(title)), "Regulation")
	if doc == nil {
		t.Fatalf("parseDocument returned nil for %q", title)
	}
	if wantSub != "" && !strings.Contains(doc.Content, wantSub) {
		t.Fatalf("heading for %q: missing %q in %q", title, wantSub, doc.Content)
	}
	if wantNotSub != "" && strings.Contains(doc.Content, wantNotSub) {
		t.Fatalf("heading for %q: unexpectedly contains %q in %q", title, wantNotSub, doc.Content)
	}
}

func TestPartHeadingDefiniteForms(t *testing.T) {
	cases := []struct {
		title   string
		want    string
		wantNot string
	}{
		{"Første delen. Sluttføresegner", "## Første delen. Sluttføresegner", "## Kapittel"},
		{"Andre delen – vidaregåande opplæring", "## Andre delen – vidaregåande opplæring", "## Kapittel"},
		{"Tredje delen – fellesreglar", "## Tredje delen – fellesreglar", "## Kapittel"},
		{"Del I. Innledende bestemmelser", "## Del I. Innledende bestemmelser", "## Kapittel"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			assertHeading(t, tc.title, tc.want, tc.wantNot)
		})
	}
}

func TestPartHeadingDoesNotOverMatchRealTitles(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Delegering", "## Kapittel 1. Delegering"},
		{"Delårsregnskap", "## Kapittel 1. Delårsregnskap"},
		{"Deling av foretak m.v.", "## Kapittel 1. Deling av foretak m.v."},
		{"Deltakelse i beslutningsprosesser", "## Kapittel 1. Deltakelse i beslutningsprosesser"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			assertHeading(t, tc.title, tc.want, "")
		})
	}
}
