package main

import (
	"reflect"
	"testing"
)

func TestNormalizeForarbeidSlug(t *testing.T) {
	cases := []struct {
		href string
		want string
		ok   bool
	}{
		{"forarbeid/prop-71-ls-202526", "prop-71-ls-202526", true},
		{"/forarbeid/prop-71-ls-202526", "prop-71-ls-202526", true},
		{"forarbeid/dok21-202021/kap10", "dok21-202021", true}, // deep link collapse
		{"forarbeid/prop-119-ls-200910/kap6.3.3.2", "prop-119-ls-200910", true},
		{"forarbeid/inns-s-196-199900#foo", "inns-s-196-199900", true}, // #anchor strip
		{"lov/2020-01-01-1", "", false},                                // not forarbeid
		{"forskrift/2020-01-01-1", "", false},
		{"forarbeid/", "", false}, // empty slug
	}
	for _, tc := range cases {
		got, ok := normalizeForarbeidSlug(tc.href)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("normalizeForarbeidSlug(%q) = (%q,%v); want (%q,%v)", tc.href, got, ok, tc.want, tc.ok)
		}
	}
}

func TestClassifyForarbeid(t *testing.T) {
	cases := []struct {
		slug                       string
		prepType, session, num, yr string
	}{
		{"otprp-76-200809", "proposisjon", "2008-2009", "76", "2008"},
		{"otprp-1-199798", "proposisjon", "1997-1998", "1", "1997"},
		{"prop-71-ls-202526", "proposisjon", "2025-2026", "71", "2025"},
		{"prop-1-s-201213", "proposisjon", "2012-2013", "1", "2012"},
		{"prop-119-ls-200910", "proposisjon", "2009-2010", "119", "2009"},
		{"inns-s-196-199900", "innstilling", "1999-2000", "196", "1999"},
		{"inns-100-l-201213", "innstilling", "2012-2013", "100", "2012"},
		{"lovvedtak-10-202223", "lovvedtak", "2022-2023", "10", "2022"},
		{"nou-1997-26", "nou", "", "26", "1997"},
		{"nou-1991-12a", "nou", "", "12a", "1991"},
		{"stprp-130-196465", "proposisjon", "1964-1965", "130", "1964"},
		{"meld-st-29-201112", "melding", "2011-2012", "29", "2011"},
		{"dok21-202021", "dok", "2020-2021", "", "2020"},
		{"dok18-202425", "dok", "2024-2025", "", "2024"},
		{"representantforslag-136-202425", "representantforslag", "2024-2025", "136", "2024"},
		{"lovanm-202425-001", "lovanm", "2024-2025", "001", "2024"},
		{"totally-unparseable", "unknown", "", "", ""},
		{"prop", "unknown", "", "", ""}, // too few parts
	}
	for _, tc := range cases {
		pt, s, n, y := classifyForarbeid(tc.slug)
		if pt != tc.prepType || s != tc.session || n != tc.num || y != tc.yr {
			t.Fatalf("classifyForarbeid(%q) = (%q,%q,%q,%q); want (%q,%q,%q,%q)",
				tc.slug, pt, s, n, y, tc.prepType, tc.session, tc.num, tc.yr)
		}
	}
}

func TestCollectForarbeidName(t *testing.T) {
	html := `<html><body>
<article class="legalP"><a href="forarbeid/prop-71-ls-202526">Prop. 71 LS (2025–2026)</a> text</article>
<article class="legalP"><a href="forarbeid/otprp-76-200809">Ot.prp.nr.76 (2008–2009)</a> more</article>
</body></html>`
	doc := parseDocument([]byte(`<!DOCTYPE html><html lang="nb"><head><title>X</title></head><body>
<header class="documentHeader" id="hode"><dl class="data-document-key-info">
<dt class="refid">RefID</dt><dd class="refid">lov/2020-01-01-1</dd>
</dl></header>
<main class="documentBody" id="dokument">`+html+`</main></body></html>`), "Law")
	if doc == nil {
		t.Fatal("nil doc")
	}
	wantRefs := []string{"otprp-76-200809", "prop-71-ls-202526"}
	if !reflect.DeepEqual(doc.ForarbeidRefs, wantRefs) {
		t.Fatalf("ForarbeidRefs = %v; want %v", doc.ForarbeidRefs, wantRefs)
	}
	if doc.ForarbeidNames["prop-71-ls-202526"] != "Prop. 71 LS (2025–2026)" {
		t.Fatalf("name = %q", doc.ForarbeidNames["prop-71-ls-202526"])
	}
}

func TestAggregatePreparatoryWorksDedupe(t *testing.T) {
	// Two docs reference the same slug; the object must be emitted once (first
	// name wins) and two PREPARES edges produced.
	docs := []LovDoc{
		{
			RefID:          "lov/2020-01-01-1",
			DocType:        "Law",
			Title:          "One",
			ForarbeidRefs:  []string{"prop-71-ls-202526"},
			ForarbeidNames: map[string]string{"prop-71-ls-202526": "Prop. 71 LS (2025–2026)"},
		},
		{
			RefID:          "lov/2020-01-01-2",
			DocType:        "Law",
			Title:          "Two",
			ForarbeidRefs:  []string{"prop-71-ls-202526"},
			ForarbeidNames: map[string]string{"prop-71-ls-202526": "SECOND TEXT"},
		},
	}
	works := aggregatePreparatoryWorks(docs)
	if len(works) != 1 {
		t.Fatalf("works = %d; want 1", len(works))
	}
	if works[0].Name != "Prop. 71 LS (2025–2026)" {
		t.Fatalf("first-wins name = %q", works[0].Name)
	}

	records := buildSeedObjectRecords(docs, nil, nil, aggregatePreparatoryWorks(docs))
	prepCount := 0
	for _, r := range records {
		if r.Type == "PreparatoryWork" {
			prepCount++
		}
	}
	if prepCount != 1 {
		t.Fatalf("PreparatoryWork objects = %d; want 1", prepCount)
	}

	objectKeys := map[string]bool{}
	for _, r := range records {
		objectKeys[r.Key] = true
	}
	rels := buildSeedRelationshipRecords(docs, nil, objectKeys)
	prepEdges := 0
	for _, r := range rels {
		if r.Type == "PREPARES" {
			prepEdges++
		}
	}
	if prepEdges != 2 {
		t.Fatalf("PREPARES edges = %d; want 2", prepEdges)
	}
}

func TestPreparesNoDanglingEdge(t *testing.T) {
	// A synthetic forarbeid reference on a doc that IS emitted resolves fine; but
	// a forarbeid slug referenced by a doc whose own object is filtered out (here
	// we simulate by checking the endpoint filter behavior) must not dangle.
	docs := []LovDoc{
		{RefID: "lov/2020-01-01-1", DocType: "Law", Title: "One", ForarbeidRefs: []string{"prop-1-s-202526"}, ForarbeidNames: map[string]string{"prop-1-s-202526": "Prop. 1 S"}},
	}
	records := buildSeedObjectRecords(docs, nil, nil, aggregatePreparatoryWorks(docs))
	objectKeys := map[string]bool{}
	for _, r := range records {
		objectKeys[r.Key] = true
	}
	rels := buildSeedRelationshipRecords(docs, nil, objectKeys)
	for _, r := range rels {
		if r.Type == "PREPARES" {
			if !objectKeys[r.SrcKey] || !objectKeys[r.DstKey] {
				t.Fatalf("PREPARES edge with missing endpoint: %+v", r)
			}
		}
	}
	// Sanity: the edge exists and both endpoints are present.
	found := false
	for _, r := range rels {
		if r.Type == "PREPARES" {
			found = true
			if r.SrcKey != "forarbeid/prop-1-s-202526" || r.DstKey != "lov/2020-01-01-1" {
				t.Fatalf("PREPARES edge wrong: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("no PREPARES edge emitted")
	}
}

func TestClassifyUnknown(t *testing.T) {
	for _, slug := range []string{"garbage", "foo-bar-baz", "prop", ""} {
		pt, s, n, y := classifyForarbeid(slug)
		if pt != "unknown" || s != "" || n != "" || y != "" {
			t.Fatalf("classifyForarbeid(%q) = (%q,%q,%q,%q); want unknown", slug, pt, s, n, y)
		}
	}
}
