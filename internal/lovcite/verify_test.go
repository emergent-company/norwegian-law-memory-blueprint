package lovcite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyQuoteMatch(t *testing.T) {
	idx := buildStandard(t, true)
	res := idx.VerifyQuote("lov/2005-06-17-62#§1-1", "Formålstekst")
	if !res.Matched {
		t.Fatalf("expected match, got %+v", res)
	}
	if res.Paragraph.SectionID != "kapittel-1-paragraf-1" {
		t.Errorf("section_id = %q", res.Paragraph.SectionID)
	}
}

func TestVerifyQuoteWhitespaceNormalised(t *testing.T) {
	// Content with runs of whitespace/newlines must still match a plain quote.
	objects := map[string]string{
		"Law.jsonl":            `{"type":"Law","key":"lov/2005-06-17-62","properties":{"content":"### § 1-1\n\nFørste   linje.\n\nAndre\nlinje."}}` + "\n",
		"LegalParagraph.jsonl": `{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"law_ref_id":"lov/2005-06-17-62","paragraph_num":"§ 1-1","section_id":"kapittel-1-paragraf-1","content":"Første   linje.\n\nAndre\nlinje."}}` + "\n",
	}
	dir := writeSeed(t, objects, map[string]string{})
	idx, err := Build(dir, BuildOptions{RetainContent: true})
	if err != nil {
		t.Fatal(err)
	}
	// Quote with collapsed whitespace matches.
	if !idx.VerifyQuote("lov/2005-06-17-62#§1-1", "Første linje. Andre linje.").Matched {
		t.Error("expected whitespace-normalised match")
	}
}

func TestVerifyQuoteMismatch(t *testing.T) {
	idx := buildStandard(t, true)
	res := idx.VerifyQuote("lov/2005-06-17-62#§1-1", "ingen slik tekst")
	if res.Matched {
		t.Fatal("expected no match")
	}
	if res.Closest == nil || res.Closest.SectionID != "kapittel-1-paragraf-1" {
		t.Errorf("closest = %+v", res.Closest)
	}
	if res.Excerpt == "" {
		t.Error("expected a context excerpt")
	}
}

func TestVerifyQuoteUnresolvedRef(t *testing.T) {
	idx := buildStandard(t, true)
	res := idx.VerifyQuote("lov/2005-06-17-62#§9-9", "whatever")
	if res.Matched {
		t.Fatal("expected no match")
	}
	if res.Closest != nil {
		t.Errorf("expected no closest when ref unresolved, got %+v", res.Closest)
	}
}

func TestBuildRetainsContentOnlyWhenAsked(t *testing.T) {
	dir := writeSeed(t, standardObjects(), standardRelationships())

	noContent, err := Build(dir, BuildOptions{RetainContent: false})
	if err != nil {
		t.Fatal(err)
	}
	p := noContent.ResolveRef("lov/2005-06-17-62#§1-1").Targets[0]
	if p.Content != "" {
		t.Errorf("content should be empty when not retained, got %q", p.Content)
	}

	withContent, err := Build(dir, BuildOptions{RetainContent: true})
	if err != nil {
		t.Fatal(err)
	}
	p2 := withContent.ResolveRef("lov/2005-06-17-62#§1-1").Targets[0]
	if p2.Content == "" {
		t.Error("content should be retained")
	}
}

func TestBuildIgnoresNonJsonl(t *testing.T) {
	dir := writeSeed(t, standardObjects(), standardRelationships())
	// Drop a stray file that Build must not choke on.
	if err := os.WriteFile(filepath.Join(dir, "objects", "README.txt"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Build(dir, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if idx.ActCount() != 2 {
		t.Errorf("ActCount = %d, want 2", idx.ActCount())
	}
}
