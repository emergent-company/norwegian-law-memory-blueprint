package main

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestEuReferenceContextClauseIsolation(t *testing.T) {
	// A multi-sentence paragraph where only the SECOND sentence mentions the
	// directive. Context/keyword must come from that later clause, not offset 0.
	text := "Første setning handler om noe helt annet. Andre setning gjennomfører direktiv 2009/103/EF i norsk rett."
	idx := strings.Index(text, "direktiv 2009/103/EF")
	if idx < 0 {
		t.Fatal("reference not found")
	}
	ctx, kw, _ := euReferenceContext(text, idx, idx+len("direktiv 2009/103/EF"), maxEUContextChars)
	if !strings.Contains(ctx, "gjennomfører") {
		t.Fatalf("context = %q; want the later clause", ctx)
	}
	if strings.Contains(ctx, "Første setning") {
		t.Fatalf("context = %q; must not include the first sentence", ctx)
	}
	if kw != "gjennomfører" {
		t.Fatalf("keyword = %q; want gjennomfører", kw)
	}
}

func TestExtractEUCelexWithPositionsIsolateClause(t *testing.T) {
	// A multi-directive EEA header: each CELEX must isolate its OWN clause, not a
	// shared offset-0 clause.
	text := "Første vedlegg gjennomfører direktiv 2009/103/EF. Andre vedlegg viser til forordning (EF) nr. 1221/2009."
	matches := extractEUCelexWithPositions(text)
	if len(matches) != 2 {
		t.Fatalf("matches = %d; want 2", len(matches))
	}
	byCELEX := make(map[string]euCelexMatch, len(matches))
	for _, m := range matches {
		byCELEX[m.CELEX] = m
	}

	dirMatch, ok := byCELEX["32009L0103"]
	if !ok {
		t.Fatalf("32009L0103 not extracted; got %v", matches)
	}
	regMatch, ok := byCELEX["32009R1221"]
	if !ok {
		t.Fatalf("32009R1221 not extracted; got %v", matches)
	}

	ctxDir, kwDir, _ := euReferenceContext(text, dirMatch.Start, dirMatch.End, maxEUContextChars)
	ctxReg, kwReg, _ := euReferenceContext(text, regMatch.Start, regMatch.End, maxEUContextChars)

	if !strings.Contains(ctxDir, "gjennomfører") || strings.Contains(ctxDir, "viser til") {
		t.Fatalf("ctxDir = %q; want the first clause only", ctxDir)
	}
	if !strings.Contains(ctxReg, "viser til") || strings.Contains(ctxReg, "gjennomfører") {
		t.Fatalf("ctxReg = %q; want the second clause only", ctxReg)
	}
	if kwDir != "gjennomfører" || kwReg != "viser til" {
		t.Fatalf("keywords = (%q, %q); want (gjennomfører, viser til)", kwDir, kwReg)
	}
}

func TestPlainTextPositionsAnchorOffset(t *testing.T) {
	doc := `<main><article class="legalArticle"><p class="legalP">Første ledd uten referanse.</p><p class="legalP">Andre ledd <a href="/eu/32009L0103">direktiv 2009/103/EF</a> gjennomføres her.</p></article></main>`
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	text, pos := plainTextPositions(root)
	anchors := findAll(root, "a")
	if len(anchors) != 1 {
		t.Fatalf("anchors = %d; want 1", len(anchors))
	}
	a := anchors[0]
	start, ok := pos[a]
	if !ok {
		t.Fatal("anchor has no recorded position")
	}
	anchorText := nodeText(a)
	if start < 0 || start+len(anchorText) > len(text) {
		t.Fatalf("anchor offset out of range: start=%d len=%d textlen=%d", start, len(anchorText), len(text))
	}
	if got := text[start : start+len(anchorText)]; got != anchorText {
		t.Fatalf("span = %q; want %q", got, anchorText)
	}
	// The anchor must sit past the first clause (not at offset 0).
	if start == 0 {
		t.Fatalf("anchor offset should not be 0")
	}
	if !strings.Contains(text[:start], "Første ledd") {
		t.Fatalf("first clause should precede the anchor")
	}
}

func TestMergeRelPropsNilDst(t *testing.T) {
	var dst map[string]any // nil
	got := mergeRelProps(dst, map[string]any{"a": 1, "b": 2})
	if got == nil {
		t.Fatal("mergeRelProps returned nil for nil dst")
	}
	if got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("merged = %v; want a,b", got)
	}

	dst2 := map[string]any{"a": 0}
	got2 := mergeRelProps(dst2, map[string]any{"b": 2, "c": 3})
	if got2["a"] != 0 || got2["b"] != 2 || got2["c"] != 3 {
		t.Fatalf("merged = %v; want first-wins a, plus b,c", got2)
	}
}
