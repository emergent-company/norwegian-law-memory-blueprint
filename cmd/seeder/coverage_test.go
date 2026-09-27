package main

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"§ 1-1", []string{"1", "1"}},
		{"Formål", []string{"formål"}},
		{"### § 1 — Formål", []string{"1", "formål"}},
		{"Ikkje i kraft", []string{"ikkje", "i", "kraft"}},
		{"Ærlig Øvelse", []string{"ærlig", "øvelse"}},
		{"", nil},
		{"1999", []string{"1999"}},
	}
	for _, tc := range cases {
		got := tokenize(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("tokenize(%q) = %v; want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("tokenize(%q) = %v; want %v", tc.in, got, tc.want)
			}
		}
	}
}

func TestComputeCoverage(t *testing.T) {
	docs := []LovDoc{
		{Coverage: 1.0},
		{Coverage: 0.5},
		{Coverage: 0.9},
	}
	s := computeCoverage(docs)
	if s.Docs != 3 || s.Min != 0.5 || s.DocsBelow099 != 2 || s.DocsBelow095 != 2 {
		t.Fatalf("computeCoverage = %+v; want docs=3 min=0.5 below099=2 below095=2", s)
	}
	if diff := s.Mean - 0.8; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("computeCoverage mean = %.17g; want 0.8", s.Mean)
	}
	if s2 := computeCoverage(nil); s2.Docs != 0 {
		t.Fatalf("computeCoverage(nil).Docs = %d; want 0", s2.Docs)
	}
}

func findMainNode(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "main" && attr(n, "id") == "dokument" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if r := findMainNode(c); r != nil {
			return r
		}
	}
	return nil
}

func TestTokenCoverage(t *testing.T) {
	// Whitespace between the header and the body paragraph keeps the two token
	// streams separable, matching extractBody's rendered "### ... — ...\n\nbody".
	docHTML := `<main class="documentBody" id="dokument"><article class="legalArticle" id="p1"><h3 class="legalArticleHeader"><span class="legalArticleValue">§ 1</span>. <span class="legalArticleTitle">Formål</span></h3> <article class="legalP">Lovens formål er å sikre trygge arbeidsforhold.</article></article></main>`
	root, err := html.Parse(strings.NewReader(docHTML))
	if err != nil {
		t.Fatal(err)
	}
	main := findMainNode(root)
	if main == nil {
		t.Fatal("no main node")
	}

	rendered := "### § 1 — Formål\n\nLovens formål er å sikre trygge arbeidsforhold."
	if c := tokenCoverage(main, rendered); c != 1.0 {
		t.Fatalf("full coverage = %v; want 1.0", c)
	}
	partial := "### § 1 — Formål\n\nformål er å sikre arbeidsforhold."
	if c := tokenCoverage(main, partial); c >= 1.0 {
		t.Fatalf("partial coverage = %v; want < 1.0", c)
	}
}

func TestManifestProvenanceAndCoverage(t *testing.T) {
	docs := []LovDoc{{RefID: "a", DocType: "Law", SourceSHA256: "h1", Coverage: 0.99}}
	cov := computeCoverage(docs)
	m := buildManifest(docs, nil, "laws", 1, 0, nil, cov)

	if m.ManifestVersion != 4 {
		t.Fatalf("ManifestVersion = %d; want 4", m.ManifestVersion)
	}
	if m.Source != "Lovdata" || m.License != "NLOD-2.0" || m.LicenseURL != "https://data.norge.no/nlod/en/2.0" {
		t.Fatalf("provenance fields wrong: %+v", m)
	}
	if !m.ChangesMade || m.NoticeFile != "NOTICE" {
		t.Fatalf("changes_made/notice_file wrong: %+v", m)
	}
	if !strings.Contains(m.Attribution, "Lovdata") || !strings.Contains(m.Attribution, "NLOD") {
		t.Fatalf("attribution missing Lovdata/NLOD: %q", m.Attribution)
	}
	if m.Coverage.Docs != 1 || m.Coverage.Mean != 0.99 || m.Coverage.Min != 0.99 || m.Coverage.DocsBelow099 != 0 || m.Coverage.DocsBelow095 != 0 {
		t.Fatalf("coverage block wrong: %+v", m.Coverage)
	}
	if m.Quality.ContentUnavailable != 0 || len(m.Quality.ContentUnavailableRefs) != 0 {
		t.Fatalf("quality block wrong for clean corpus: %+v", m.Quality)
	}
}
