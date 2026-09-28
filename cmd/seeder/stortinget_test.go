package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseStortingetIndexJSON(t *testing.T) {
	listJSON := `{"publikasjoner_liste":[{"id":"inns-200405-116","tittel":"Innst. 116 (2004-2005)","type":6},{"id":"inno-200405-080","tittel":"Innst. O. nr. 80 (2004-2005)","type":6}]}`
	entries, err := parseStortingetIndexJSON([]byte(listJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != "inns-200405-116" {
		t.Fatalf("entries = %+v", entries)
	}

	singleJSON := `{"publikasjoner_liste":{"id":"vedtak-202526-001","tittel":"Lovvedtak 1 (2025-2026)","type":7}}`
	entries, err = parseStortingetIndexJSON([]byte(singleJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "vedtak-202526-001" {
		t.Fatalf("single entries = %+v", entries)
	}
}

func TestParseInnstillingSlug(t *testing.T) {
	cases := []struct {
		slug    string
		variant string
		num     string
		session string
	}{
		{"inns-o-80-200405", "o", "80", "200405"},
		{"inns-s-116-200405", "s", "116", "200405"},
		{"inns-100-l-201213", "l", "100", "201213"},
	}
	for _, tc := range cases {
		v, n, s := parseInnstillingSlug(strings.Split(tc.slug, "-"))
		if v != tc.variant || n != tc.num || s != tc.session {
			t.Fatalf("parseInnstillingSlug(%q) = (%q,%q,%q); want (%q,%q,%q)", tc.slug, v, n, s, tc.variant, tc.num, tc.session)
		}
	}
}

func TestMatchStortingetPub(t *testing.T) {
	idx := stortingetIndex{
		"inno-200405-080":   {ID: "inno-200405-080", Tittel: "Innst. O. nr. 80 (2004-2005)"},
		"inns-200405-116":   {ID: "inns-200405-116", Tittel: "Innst. 116 (2004-2005)"},
		"inns-200405-080":   {ID: "inns-200405-080", Tittel: "Innst. 80 (2004-2005)"},
		"vedtak-202526-001": {ID: "vedtak-202526-001", Tittel: "Lovvedtak 1 (2025-2026)"},
	}

	// Variant is load-bearing: inns-o-80 -> inno, NOT inns.
	pw := prepWork{Slug: "inns-o-80-200405", Name: "Innst. O. nr. 80 (2004-2005)", PrepType: "innstilling", Session: "2004-2005", DocNumber: "80"}
	pub, ok, _ := matchStortingetPub(pw, idx)
	if !ok || pub.ID != "inno-200405-080" {
		t.Fatalf("inns-o-80 -> %v ok=%v (want inno-200405-080)", pub, ok)
	}

	pw = prepWork{Slug: "inns-s-116-200405", Name: "Innst. S. nr. 116 (2004-2005)", PrepType: "innstilling", Session: "2004-2005", DocNumber: "116"}
	pub, ok, _ = matchStortingetPub(pw, idx)
	if !ok || pub.ID != "inns-200405-116" {
		t.Fatalf("inns-s-116 -> %v ok=%v (want inns-200405-116)", pub, ok)
	}

	pw = prepWork{Slug: "lovvedtak-1-202526", Name: "Lovvedtak 1 (2025-2026)", PrepType: "lovvedtak", Session: "2025-2026", DocNumber: "1"}
	pub, ok, _ = matchStortingetPub(pw, idx)
	if !ok || pub.ID != "vedtak-202526-001" {
		t.Fatalf("lovvedtak-1 -> %v ok=%v (want vedtak-202526-001)", pub, ok)
	}

	// Title mismatch (number 180 not in any title) -> rejected.
	pw = prepWork{Slug: "inns-s-180-200405", Name: "Innst. S. nr. 180 (2004-2005)", PrepType: "innstilling", Session: "2004-2005", DocNumber: "180"}
	if _, ok, _ := matchStortingetPub(pw, idx); ok {
		t.Fatalf("inns-s-180 should not match")
	}
}

func TestInnstillingCandidates(t *testing.T) {
	cases := []struct {
		variant string
		want    []string
	}{
		{"o", []string{"inno-200405-080"}},
		{"l", []string{"inns-200405-080l", "inns-200405-080"}},
		{"s", []string{"inns-200405-080s", "inns-200405-080"}},
		{"", []string{"inns-200405-080"}},
	}
	for _, tc := range cases {
		got := innstillingCandidates(tc.variant, "200405", "80")
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("innstillingCandidates(%q) = %v; want %v", tc.variant, got, tc.want)
		}
	}
}

func TestMatchStortingetPubLAndSSuffix(t *testing.T) {
	idx := stortingetIndex{
		"inns-202223-103l": {ID: "inns-202223-103l", Tittel: "Innst. 103 L (2022-2023)"},
		"inns-202223-100s": {ID: "inns-202223-100s", Tittel: "Innst. 100 S (2022-2023)"},
		// Pre-suffix era (2009–2015): the L/S classification is absent from the
		// id, so "Innst. 100 L (2012-2013)" lives under the plain id.
		"inns-201213-100": {ID: "inns-201213-100", Tittel: "Innst. 100 (2012-2013)"},
	}

	// l suffix (2016+): must resolve to the -l id, not the plain one.
	pw := prepWork{Slug: "inns-103-l-202223", Name: "Innst.103 L (2022–2023)", PrepType: "innstilling", Session: "2022-2023", DocNumber: "103", Year: "2022"}
	pub, ok, _ := matchStortingetPub(pw, idx)
	if !ok || pub.ID != "inns-202223-103l" {
		t.Fatalf("inns-103-l -> %v ok=%v (want inns-202223-103l)", pub, ok)
	}

	// s suffix (2016+).
	pw = prepWork{Slug: "inns-100-s-202223", Name: "Innst. 100 S (2022–2023)", PrepType: "innstilling", Session: "2022-2023", DocNumber: "100", Year: "2022"}
	pub, ok, _ = matchStortingetPub(pw, idx)
	if !ok || pub.ID != "inns-202223-100s" {
		t.Fatalf("inns-100-s -> %v ok=%v (want inns-202223-100s)", pub, ok)
	}

	// l in pre-suffix era: falls back to the plain id.
	pw = prepWork{Slug: "inns-100-l-201213", Name: "Innst.100 L (2012–2013)", PrepType: "innstilling", Session: "2012-2013", DocNumber: "100", Year: "2012"}
	pub, ok, _ = matchStortingetPub(pw, idx)
	if !ok || pub.ID != "inns-201213-100" {
		t.Fatalf("inns-100-l pre-suffix -> %v ok=%v (want inns-201213-100)", pub, ok)
	}
}

func TestMatchStortingetPubPre1999(t *testing.T) {
	idx := stortingetIndex{}
	pw := prepWork{Slug: "inns-s-212-199394", Name: "Innst. S. nr. 212 (1993-94", PrepType: "innstilling", Session: "1993-1994", DocNumber: "212", Year: "1993"}
	_, ok, reason := matchStortingetPub(pw, idx)
	if ok || reason != "pre_1999" {
		t.Fatalf("pre-1999 innstilling: ok=%v reason=%q; want ok=false reason=pre_1999", ok, reason)
	}
}

func TestTitleMatchesRejection(t *testing.T) {
	// "80" must not match "180" (word-boundary).
	if titleMatches("Innst. 180 (2004-2005)", "Innst. 80 (2004-2005)", "80") {
		t.Fatalf("titleMatches should reject 80 vs 180")
	}
	if !titleMatches("Innst. 80 (2004-2005)", "Innst. 80 (2004-2005)", "80") {
		t.Fatalf("titleMatches should accept exact 80")
	}
	if titleMatches("", "Innst. 80", "80") {
		t.Fatalf("empty title should reject")
	}
}

func TestExtractStortingetMarkdownLowercase(t *testing.T) {
	xml := `<innstilling id="bi001-05"><front><titgrp><innst>Budsjett-innst. S. nr. 1</innst><aar>(2004-2005)</aar><doktit>Budsjettinnstilling til Stortinget</doktit><kildedok>St.meld. nr. 1 (2004-2005)</kildedok></titgrp></front><til><kapittel><tit>1. Innledning</tit><uttalelse><a type="innrykk"><uttal>Komiteen viser til</uttal></a></uttalelse></kapittel></til></innstilling>`
	md, err := extractStortingetMarkdown([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "### 1. Innledning") {
		t.Fatalf("missing heading: %q", md)
	}
	if !strings.Contains(md, "Komiteen viser til") {
		t.Fatalf("missing body text: %q", md)
	}
	if strings.Contains(md, "Budsjett-innst. S. nr. 1") {
		// The <innst> tag is inline; its text may or may not be present, but the
		// doktit heading must be.
	}
	if !strings.Contains(md, "### Budsjettinnstilling til Stortinget") {
		t.Fatalf("missing doktit heading: %q", md)
	}
}

func TestExtractStortingetMarkdownCapitalized(t *testing.T) {
	xml := `<Innstilling Status="Komplett"><Startseksjon><Navn>Lovvedtak 1</Navn><Aar>(2025–2026)</Aar><Kildedok>Innst. 33 L (2025–2026), jf. Prop. 158 L (2024–2025)</Kildedok></Startseksjon><Sluttseksjon><VedtakTilLov><Tittel>vedtak til lov</Tittel><Paragraf><Tittel>§ 4-3 a</Tittel><A Type="Innrykk"><Endring>Med leige til eige er det i denne lova meint avtale</Endring></A></Paragraf></VedtakTilLov></Sluttseksjon></Innstilling>`
	md, err := extractStortingetMarkdown([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "### vedtak til lov") {
		t.Fatalf("missing vedtak heading: %q", md)
	}
	if !strings.Contains(md, "Med leige til eige er det i denne lova meint avtale") {
		t.Fatalf("missing paragraf body: %q", md)
	}
}

func TestKildedokToSlugs(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"Prop. 158 L (2024–2025)", []string{"prop-158-l-202425"}},
		{"Ot.prp. nr. 76 (2008-2009)", []string{"otprp-76-200809"}},
		{"St.meld. nr. 1 (2004-2005)", []string{"meld-st-1-200405"}},
		{"Innst. 33 L (2025–2026)", []string{"inns-33-l-202526"}},
		{"NOU 2017:15", []string{"nou-2017-15"}},
	}
	for _, tc := range cases {
		got := kildedokToSlugs(tc.text)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("kildedokToSlugs(%q) = %v; want %v", tc.text, got, tc.want)
		}
	}
}

func TestKildedokToSlugsDedupeSort(t *testing.T) {
	got := kildedokToSlugs("Prop. 158 L (2024–2025), jf. Prop. 100 L (2024–2025), Prop. 158 L (2024–2025)")
	if !reflect.DeepEqual(got, []string{"prop-100-l-202425", "prop-158-l-202425"}) {
		t.Fatalf("got %v", got)
	}
}

func TestBuildDerivesFromEdges(t *testing.T) {
	preps := []prepWork{
		{Slug: "inns-s-33-202526", PrepType: "innstilling", ContentAvailable: true,
			KildedokSlugs: []string{"prop-158-l-202425", "nou-2017-15"}},
		// dangling target: nou-9999-1 not in corpus -> no edge.
		{Slug: "inns-s-34-202526", PrepType: "innstilling", ContentAvailable: true,
			KildedokSlugs: []string{"nou-9999-1"}},
		// self-loop must be skipped.
		{Slug: "inns-s-35-202526", PrepType: "innstilling", ContentAvailable: true,
			KildedokSlugs: []string{"inns-s-35-202526"}},
		// not enriched -> no edges even with valid targets.
		{Slug: "inns-s-36-202526", PrepType: "innstilling",
			KildedokSlugs: []string{"prop-158-l-202425"}},
		// targets present in corpus.
		{Slug: "prop-158-l-202425", PrepType: "proposisjon"},
		{Slug: "nou-2017-15", PrepType: "nou"},
	}

	edges := buildDerivesFromEdges(preps)
	if len(edges) != 2 {
		t.Fatalf("edges = %d; want 2 (dangling + self-loop + unenriched skipped): %+v", len(edges), edges)
	}
	// Deterministic order: inns-s-33 before inns-s-34 (only 33 has valid edges).
	if edges[0].SrcKey != "forarbeid/inns-s-33-202526" || edges[0].DstKey != "forarbeid/nou-2017-15" {
		t.Fatalf("edge[0] = %+v; want nou target (sorted kildedok slugs)", edges[0])
	}
	if edges[1].DstKey != "forarbeid/prop-158-l-202425" {
		t.Fatalf("edge[1] = %+v; want prop target", edges[1])
	}
	for _, e := range edges {
		if e.Type != "DERIVES_FROM" {
			t.Fatalf("edge type = %q", e.Type)
		}
	}
}

func TestBuildDerivesFromEdgesDeterministic(t *testing.T) {
	// Production input is slug-sorted (aggregatePreparatoryWorks sorts slugs).
	preps := []prepWork{
		{Slug: "inns-a-1-202526", PrepType: "innstilling", ContentAvailable: true,
			KildedokSlugs: []string{"prop-1-l-202425"}},
		{Slug: "inns-b-2-202526", PrepType: "innstilling", ContentAvailable: true,
			KildedokSlugs: []string{"prop-2-l-202425", "prop-1-l-202425"}},
		{Slug: "prop-1-l-202425", PrepType: "proposisjon"},
		{Slug: "prop-2-l-202425", PrepType: "proposisjon"},
	}
	a := buildDerivesFromEdges(preps)
	b := buildDerivesFromEdges(preps)
	if len(a) != 3 || !reflect.DeepEqual(a, b) {
		t.Fatalf("nondeterministic: a=%+v b=%+v", a, b)
	}
	// Sorted by (src slug, then kildedok slug) — inns-a-1 first.
	if a[0].SrcKey != "forarbeid/inns-a-1-202526" {
		t.Fatalf("a[0].SrcKey = %q; want inns-a-1 first", a[0].SrcKey)
	}
	// kildedok slugs sorted per source: prop-1 before prop-2.
	if a[1].SrcKey != "forarbeid/inns-b-2-202526" || a[1].DstKey != "forarbeid/prop-1-l-202425" {
		t.Fatalf("a[1] = %+v; want inns-b-2 -> prop-1", a[1])
	}
}
