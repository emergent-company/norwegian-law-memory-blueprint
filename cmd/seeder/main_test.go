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

func TestNormalizeDateTimeValue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
		ok   bool
	}{
		{"clock time no seconds", "2024-07-09 14:55", "2024-07-09T14:55:00Z", true},
		{"date only", "2024-07-09", "2024-07-09T00:00:00Z", true},
		{"free text", "not a date", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeDateTimeValue(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("normalizeDateTimeValue(%v) = (%q, %v); want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
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
