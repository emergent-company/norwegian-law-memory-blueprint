package main

import (
	"reflect"
	"testing"
)

func TestClassifyEUReferenceType(t *testing.T) {
	cases := []struct {
		clause      string
		wantType    string
		wantKeyword string
	}{
		{"Loven gjennomfører direktiv 2009/103/EF.", "implements", "gjennomfører"},
		{"Reglene bygger på gjennomføring av direktivet.", "implements", "gjennomføring"},
		{"Forskriften supplerer forordningen.", "supplements", "supplerer"},
		{"Forskriften utfyller direktivet.", "supplements", "utfyller"},
		{"Forordningen gjelder direkte i norsk rett.", "applies", "gjelder direkte"},
		{"Forordningen får direkte anvendelse her.", "applies", "får direkte anvendelse"},
		{"Forordningen anvendes direkte.", "applies", "anvendes direkte"},
		{"Dette er i samsvar med direktivet.", "complies_with", "i samsvar med"},
		{"Loven oppfyller kravene.", "complies_with", "oppfyller"},
		{"Loven etterlever forpliktelsene.", "complies_with", "etterlever"},
		{"Dette er et fravik fra direktivet.", "derogates_from", "fravik"},
		{"Direktivet er endret ved direktiv 2014/40/EU.", "amended_by", "endret ved"},
		{"Endring av direktivet trer i kraft.", "amended_by", "endring av"},
		{"Direktivet er opphevet ved direktiv 2015/1535/EU.", "repealed_by", "opphevet ved"},
		{"Oppheving av direktivet trer i kraft.", "repealed_by", "oppheving av"},
		{"Loven viser til direktivet.", "references", "viser til"},
		{"Loven henviser til forordningen.", "references", "henviser til"},
		{"Loven refererer til direktivet.", "references", "refererer til"},
		{"En helt nøytral setning uten EU-henvisning.", "references", ""},
	}
	for _, tc := range cases {
		gotType, gotKw := classifyEUReferenceType(tc.clause)
		if gotType != tc.wantType || gotKw != tc.wantKeyword {
			t.Fatalf("classifyEUReferenceType(%q) = (%q, %q); want (%q, %q)",
				tc.clause, gotType, gotKw, tc.wantType, tc.wantKeyword)
		}
	}
}

func TestExtractEUArticles(t *testing.T) {
	cases := []struct {
		clause string
		want   []string
	}{
		{"Jf. artikkel 6.1.a.", []string{"6.1.a"}},
		{"Jf. art. 6.", []string{"6"}},
		{"Jf. artikkel 6 nr. 1 bokstav a.", []string{"6.1.a"}},
		{"Jf. artikkel 6 nr. 1 bokstav a og c.", []string{"6.1.a", "6.1.c"}},
		{"Jf. artikkel 13-15.", []string{"13-15"}},
		{"Jf. artikkel 6, 7, 8.", []string{"6", "7", "8"}},
		{"Jf. artikkel 6.1.a, 6.1.c.", []string{"6.1.a", "6.1.c"}},
		{"Jf. direktivets artikler generelt.", nil},
		{"Ingen henvisning her.", nil},
	}
	for _, tc := range cases {
		got := extractEUArticles(tc.clause)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("extractEUArticles(%q) = %v; want %v", tc.clause, got, tc.want)
		}
	}
}

func TestExtractEUArticlesDedupeSort(t *testing.T) {
	// Repeats and out-of-order forms collapse to a stable ascending list.
	clause := "artikkel 13-15 og art. 6 og artikkel 6.1.a og art. 6"
	got := extractEUArticles(clause)
	want := []string{"6", "6.1.a", "13-15"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}

func TestIsPrimaryRef(t *testing.T) {
	cases := map[string]bool{
		"implements":     true,
		"supplements":    true,
		"applies":        false,
		"complies_with":  false,
		"derogates_from": false,
		"amended_by":     false,
		"repealed_by":    false,
		"references":     false,
	}
	for refType, want := range cases {
		if got := isPrimaryRef(refType); got != want {
			t.Fatalf("isPrimaryRef(%q) = %v; want %v", refType, got, want)
		}
	}
}

func TestEuReferenceContextEmpty(t *testing.T) {
	// No text / invalid range must never fabricate a context, keyword or articles.
	ctx, kw, arts := euReferenceContext("", 0, 0, maxEUContextChars)
	if ctx != "" || kw != "" || arts != nil {
		t.Fatalf("euReferenceContext(empty) = (%q, %q, %v); want empty", ctx, kw, arts)
	}
	ctx, kw, arts = euReferenceContext("noe tekst", -1, 2, maxEUContextChars)
	if ctx != "" || kw != "" || arts != nil {
		t.Fatalf("euReferenceContext(invalid) = (%q, %q, %v); want empty", ctx, kw, arts)
	}
}

func TestEuReferenceContextNoFabrication(t *testing.T) {
	// A clause with no EU keyword yields empty keyword and no articles.
	ctx, kw, arts := euReferenceContext("En nøytral setning uten nøkkelord.", 0, 30, maxEUContextChars)
	if ctx == "" {
		t.Fatalf("context should be present")
	}
	if kw != "" || arts != nil {
		t.Fatalf("keyword/articles fabricated: (%q, %v)", kw, arts)
	}
}

func TestBuildSeedRelationshipsNoEUFabrication(t *testing.T) {
	// A doc with no EU references produces no EU edges at all.
	docs := []LovDoc{{RefID: "lov/2020-01-01-1", DocType: "Law"}}
	records := buildSeedRelationshipRecords(docs, nil, map[string]bool{"lov/2020-01-01-1": true})
	for _, r := range records {
		if r.Type == "IMPLEMENTS_EEA" || r.Type == "CITES_EU_LAW" {
			t.Fatalf("no-EU doc produced %s edge: %+v", r.Type, r)
		}
	}
}

func TestCitesEULawDefaultsNoFabrication(t *testing.T) {
	// A body citation with no recovered metadata emits only deterministic
	// defaults — no fabricated articles/context/keyword.
	docs := []LovDoc{{
		RefID:      "lov/2020-01-01-1",
		DocType:    "Law",
		EUBodyRefs: []string{"32016R0679"},
	}}
	records := buildSeedRelationshipRecords(docs, nil, map[string]bool{})
	found := false
	for _, r := range records {
		if r.Type != "CITES_EU_LAW" {
			continue
		}
		found = true
		if got := r.Properties["reference_type"]; got != "cites_article" {
			t.Fatalf("reference_type = %v; want cites_article", got)
		}
		if got := r.Properties["eu_articles"]; got != "" {
			t.Fatalf("eu_articles = %v; want empty", got)
		}
		if got := r.Properties["reference_context"]; got != "" {
			t.Fatalf("reference_context = %v; want empty", got)
		}
		if got := r.Properties["trigger_keyword"]; got != "" {
			t.Fatalf("trigger_keyword = %v; want empty", got)
		}
	}
	if !found {
		t.Fatalf("expected a CITES_EU_LAW edge")
	}
}

func TestImplementsEEAProperties(t *testing.T) {
	// Header-sourced edge carries the full taxonomy, with implementation_status
	// always "unknown" and is_primary derived from reference_type.
	docs := []LovDoc{{
		RefID:      "lov/2020-01-01-1",
		DocType:    "Law",
		EUCelexIDs: []string{"32009L0103"},
		EEAMeta: map[string]euRefMeta{
			"32009L0103": {Context: "Loven gjennomfører direktiv 2009/103/EF.", Keyword: "gjennomfører", Articles: []string{"6.1.a"}},
		},
	}}
	dirs := []*EUDirective{{CelexID: "32009L0103"}}
	records := buildSeedRelationshipRecords(docs, dirs, map[string]bool{})
	for _, r := range records {
		if r.Type != "IMPLEMENTS_EEA" {
			continue
		}
		if got := r.Properties["source"]; got != "eea_references" {
			t.Fatalf("source = %v", got)
		}
		if got := r.Properties["reference_type"]; got != "implements" {
			t.Fatalf("reference_type = %v; want implements", got)
		}
		if got := r.Properties["is_primary"]; got != true {
			t.Fatalf("is_primary = %v; want true", got)
		}
		if got := r.Properties["implementation_status"]; got != "unknown" {
			t.Fatalf("implementation_status = %v; want unknown", got)
		}
		if got := r.Properties["eu_articles"]; got != "6.1.a" {
			t.Fatalf("eu_articles = %v; want 6.1.a", got)
		}
		if got := r.Properties["trigger_keyword"]; got != "gjennomfører" {
			t.Fatalf("trigger_keyword = %v", got)
		}
	}
}

func TestImplementsEEAHeaderOnlyDefaults(t *testing.T) {
	// A header reference with no trigger keyword is still a transposition
	// declaration: reference_type defaults to "implements", is_primary true.
	docs := []LovDoc{{
		RefID:      "lov/2020-01-01-1",
		DocType:    "Law",
		EUCelexIDs: []string{"32009L0103"},
		EEAMeta: map[string]euRefMeta{
			"32009L0103": {Context: "EØS-avtalen vedlegg IX nr. 8 (direktiv 2009/103/EF).", Keyword: "", Articles: nil},
		},
	}}
	dirs := []*EUDirective{{CelexID: "32009L0103"}}
	records := buildSeedRelationshipRecords(docs, dirs, map[string]bool{})
	found := false
	for _, r := range records {
		if r.Type != "IMPLEMENTS_EEA" {
			continue
		}
		found = true
		if got := r.Properties["reference_type"]; got != "implements" {
			t.Fatalf("reference_type = %v; want implements", got)
		}
		if got := r.Properties["is_primary"]; got != true {
			t.Fatalf("is_primary = %v; want true", got)
		}
		if got := r.Properties["trigger_keyword"]; got != "" {
			t.Fatalf("trigger_keyword = %v; want empty", got)
		}
	}
	if !found {
		t.Fatalf("expected an IMPLEMENTS_EEA edge")
	}
}

func TestImplementsEEAExplicitNonPrimary(t *testing.T) {
	// An explicit keyword keeps its mapped reference_type; is_primary is true
	// only for implements/supplements.
	docs := []LovDoc{{
		RefID:      "lov/2020-01-01-1",
		DocType:    "Law",
		EUCelexIDs: []string{"32009R0469"},
		EEAMeta: map[string]euRefMeta{
			"32009R0469": {Context: "Forordningen gjelder direkte.", Keyword: "gjelder direkte"},
		},
	}}
	dirs := []*EUDirective{{CelexID: "32009R0469"}}
	records := buildSeedRelationshipRecords(docs, dirs, map[string]bool{})
	found := false
	for _, r := range records {
		if r.Type != "IMPLEMENTS_EEA" {
			continue
		}
		found = true
		if got := r.Properties["reference_type"]; got != "applies" {
			t.Fatalf("reference_type = %v; want applies", got)
		}
		if got := r.Properties["is_primary"]; got != false {
			t.Fatalf("is_primary = %v; want false", got)
		}
	}
	if !found {
		t.Fatalf("expected an IMPLEMENTS_EEA edge")
	}
}

func TestDedupeSeedRelationshipsMergesProps(t *testing.T) {
	in := []seedRelationshipRecord{
		{Type: "CITES_EU_LAW", SrcKey: "a", DstKey: "b", Properties: map[string]any{"reference_type": "cites_article"}},
		{Type: "CITES_EU_LAW", SrcKey: "a", DstKey: "b", Properties: map[string]any{"reference_type": "references", "eu_articles": "6.1.a", "reference_context": "x"}},
	}
	out := dedupeSeedRelationships(in)
	if len(out) != 1 {
		t.Fatalf("len = %d; want 1", len(out))
	}
	p := out[0].Properties
	if p["reference_type"] != "cites_article" {
		t.Fatalf("reference_type = %v; want first-wins cites_article", p["reference_type"])
	}
	if p["eu_articles"] != "6.1.a" {
		t.Fatalf("eu_articles = %v; want merged", p["eu_articles"])
	}
	if p["reference_context"] != "x" {
		t.Fatalf("reference_context = %v; want merged", p["reference_context"])
	}
}
