package main

import (
	"testing"
)

func TestNorwegianMonthNum(t *testing.T) {
	cases := map[string]int{
		"januar": 1, "februar": 2, "mars": 3, "april": 4,
		"mai": 5, "juni": 6, "juli": 7, "august": 8,
		"september": 9, "oktober": 10, "november": 11, "desember": 12,
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "jun": 6, "jul": 7,
		"aug": 8, "sep": 9, "okt": 10, "nov": 11, "des": 12,
		"JANUAR": 1, "Desember": 12, "  mars  ": 3,
		"foo": 0, "": 0,
	}
	for in, want := range cases {
		if got := norwegianMonthNum(in); got != want {
			t.Fatalf("norwegianMonthNum(%q) = %d; want %d", in, got, want)
		}
	}
}

func TestParseAmendmentNoteKinds(t *testing.T) {
	cases := []struct {
		text   string
		kind   string
		date   string
		lovRef string
		ok     bool
	}{
		{"Endret ved lov 14 juni 2024 nr. 20", "endret", "2024-06-14", "lov/2024-06-14-20", true},
		{"Endret ved lov 14. juni 2024 nr. 20", "endret", "2024-06-14", "lov/2024-06-14-20", true},
		{"Endra ved lov 22 des 2021 nr. 161", "endret", "2021-12-22", "lov/2021-12-22-161", true},
		{"Opphevet ved lov 15 mai 2009 nr. 28", "opphevet", "2009-05-15", "lov/2009-05-15-28", true},
		{"Tilføyd ved lov 20 des 2019 nr. 53", "tilfoyd", "2019-12-20", "lov/2019-12-20-53", true},
		{"Ny lydelse ved lov 20 des 2019 nr. 53", "ny_lydelse", "2019-12-20", "lov/2019-12-20-53", true},
		{"Ny ordlyd ved lov 1 jan 2020 nr. 5", "ny_lydelse", "2020-01-01", "lov/2020-01-01-5", true},
		// single-digit day
		{"Endret ved lov 4 april 2018 nr. 9", "endret", "2018-04-04", "lov/2018-04-04-9", true},
	}
	for _, tc := range cases {
		kind, date, lovRef, ok := parseAmendmentNote(tc.text)
		if ok != tc.ok || kind != tc.kind || date != tc.date || lovRef != tc.lovRef {
			t.Fatalf("parseAmendmentNote(%q) = (%q,%q,%q,%v); want (%q,%q,%q,%v)",
				tc.text, kind, date, lovRef, ok, tc.kind, tc.date, tc.lovRef, tc.ok)
		}
	}
}

func TestParseAmendmentNoteNoNote(t *testing.T) {
	cases := []string{
		"",
		"En helt vanlig setning uten note.",
		"Lovens § 5 første ledd.",
		"EØS-avtalen vedlegg IX nr. 8 (direktiv 2009/103/EF).",
	}
	for _, text := range cases {
		if _, _, _, ok := parseAmendmentNote(text); ok {
			t.Fatalf("parseAmendmentNote(%q) ok=true; want false", text)
		}
	}
}

func TestParseAmendmentNoteNoDateStillNote(t *testing.T) {
	// "Ny lydelse" without a "ved lov … nr. N" suffix is still a note, but the
	// date/ref are empty (never fabricated).
	kind, date, lovRef, ok := parseAmendmentNote("Ny lydelse skal lyde:")
	if !ok || kind != "ny_lydelse" || date != "" || lovRef != "" {
		t.Fatalf("got (%q,%q,%q,%v); want (ny_lydelse,\"\",\"\",true)", kind, date, lovRef, ok)
	}
}

func TestDeriveLawAmendments(t *testing.T) {
	d := LovDoc{
		RefID:             "lov/2020-01-01-1",
		LastChangedByRef:  "lov/2023-06-01-2",
		LastChangeInForce: "2023-06-01",
		AmendsRefs:        []string{"lov/2015-01-01-3", "lov/2016-01-01-4"},
	}
	got := deriveLawAmendments(d)
	if len(got) != 3 {
		t.Fatalf("len = %d; want 3", len(got))
	}
	// LastChangedByRef record first.
	r := got[0]
	if r.AmendedByRefID != "lov/2023-06-01-2" || r.AmendmentDate != "2023-06-01" ||
		r.TargetLawRefID != "lov/2020-01-01-1" || r.AmendmentType != "unknown" || r.Source != "metadata" {
		t.Fatalf("LastChangedByRef record = %+v", r)
	}
	// AmendsRefs records: amending doc -> target.
	if got[1].TargetLawRefID != "lov/2015-01-01-3" || got[1].AmendedByRefID != "lov/2020-01-01-1" {
		t.Fatalf("AmendsRefs[0] = %+v", got[1])
	}
	if got[2].TargetLawRefID != "lov/2016-01-01-4" || got[2].AmendedByRefID != "lov/2020-01-01-1" {
		t.Fatalf("AmendsRefs[1] = %+v", got[2])
	}
	for i, r := range got[1:] {
		if r.AmendmentDate != "" {
			t.Fatalf("AmendsRefs[%d].AmendmentDate = %q; want empty", i, r.AmendmentDate)
		}
	}
}

func TestDeriveLawAmendmentsNoSignals(t *testing.T) {
	d := LovDoc{RefID: "lov/2020-01-01-1"}
	if got := deriveLawAmendments(d); len(got) != 0 {
		t.Fatalf("deriveLawAmendments = %+v; want empty", got)
	}
	if got := bodyNoteAmendments(d); len(got) != 0 {
		t.Fatalf("bodyNoteAmendments = %+v; want empty", got)
	}
}

func TestBodyNoteAmendments(t *testing.T) {
	d := LovDoc{
		RefID: "lov/2020-01-01-1",
		Paragraphs: []LovParagraph{
			{SectionID: "kapittel-1-paragraf-2", ParagraphNum: "§ 8-10", Title: "Foo", Content: "Bestemmelsen er endret ved lov 14 juni 2024 nr. 20."},
			{SectionID: "kapittel-1-paragraf-3", ParagraphNum: "§ 8-11", Content: "Ingen note her."},
		},
	}
	got := bodyNoteAmendments(d)
	if len(got) != 1 {
		t.Fatalf("len = %d; want 1", len(got))
	}
	r := got[0]
	if r.AmendmentType != "endret" || r.AmendmentDate != "2024-06-14" || r.AmendedByRefID != "lov/2024-06-14-20" {
		t.Fatalf("record = %+v", r)
	}
	if r.TargetSectionID != "kapittel-1-paragraf-2" || r.TargetLawRefID != "lov/2020-01-01-1" || r.Source != "body_note" {
		t.Fatalf("record = %+v", r)
	}
	if r.TargetProvisionRef != "§ 8-10" {
		t.Fatalf("TargetProvisionRef = %q; want § 8-10", r.TargetProvisionRef)
	}
	if r.ChangeSummary == "" {
		t.Fatalf("ChangeSummary empty; want the note text")
	}
}

func TestAmendmentAndVersionKeysDeterministic(t *testing.T) {
	a1 := amendmentKey("lov/2020-01-01-1", "lov/2023-06-01-2", "", "metadata")
	a2 := amendmentKey("lov/2020-01-01-1", "lov/2023-06-01-2", "", "metadata")
	if a1 != a2 {
		t.Fatalf("amendmentKey not deterministic: %q vs %q", a1, a2)
	}
	if a1 != "amend_lov_2020_01_01_1_lov_2023_06_01_2__metadata" {
		t.Fatalf("amendmentKey = %q", a1)
	}
	v1 := versionKey("lov/2020-01-01-1", "kapittel-1-paragraf-2")
	v2 := versionKey("lov/2020-01-01-1", "kapittel-1-paragraf-2")
	if v1 != v2 {
		t.Fatalf("versionKey not deterministic")
	}
	if v1 != "ver_lov_2020_01_01_1_kapittel_1_paragraf_2" {
		t.Fatalf("versionKey = %q", v1)
	}
}

func TestSanitizeKey(t *testing.T) {
	cases := map[string]string{
		"lov/2020-01-01-1":  "lov_2020_01_01_1",
		"lov/2024-06-14-20": "lov_2024_06_14_20",
		"§ 8-10":            "__8_10",
		"":                  "",
	}
	for in, want := range cases {
		if got := sanitizeKey(in); got != want {
			t.Fatalf("sanitizeKey(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestBuildSeedRecordsNoAmendmentSignals(t *testing.T) {
	// A doc with no amendment signals produces no Amendment/ProvisionVersion
	// objects and no HAS_AMENDMENT/HAS_VERSION edges.
	docs := []LovDoc{{RefID: "lov/2020-01-01-1", DocType: "Law"}}
	objs := buildSeedObjectRecords(docs, nil, nil, nil)
	for _, o := range objs {
		if o.Type == "Amendment" || o.Type == "ProvisionVersion" {
			t.Fatalf("no-signal doc produced %s object: %+v", o.Type, o)
		}
	}
	rels := buildSeedRelationshipRecords(docs, nil, map[string]bool{"lov/2020-01-01-1": true})
	for _, r := range rels {
		if r.Type == "HAS_AMENDMENT" || r.Type == "HAS_VERSION" {
			t.Fatalf("no-signal doc produced %s edge", r.Type)
		}
	}
}

func TestAmendmentObjectProperties(t *testing.T) {
	docs := []LovDoc{{
		RefID:             "lov/2020-01-01-1",
		DocType:           "Law",
		Title:             "Testloven",
		LastChangedByRef:  "lov/2023-06-01-2",
		LastChangeInForce: "2023-06-01",
	}}
	objs := buildSeedObjectRecords(docs, nil, nil, nil)
	var am *seedObjectRecord
	for i := range objs {
		if objs[i].Type == "Amendment" {
			am = &objs[i]
			break
		}
	}
	if am == nil {
		t.Fatalf("no Amendment object emitted")
	}
	if am.Properties["amendment_type"] != "unknown" {
		t.Fatalf("amendment_type = %v", am.Properties["amendment_type"])
	}
	if am.Properties["source"] != "metadata" {
		t.Fatalf("source = %v", am.Properties["source"])
	}
	if am.Properties["target_law_ref_id"] != "lov/2020-01-01-1" {
		t.Fatalf("target_law_ref_id = %v", am.Properties["target_law_ref_id"])
	}
	if am.Properties["amended_by_ref_id"] != "lov/2023-06-01-2" {
		t.Fatalf("amended_by_ref_id = %v", am.Properties["amended_by_ref_id"])
	}
	// amendment_date is normalized to a bare date.
	if am.Properties["amendment_date"] != "2023-06-01" {
		t.Fatalf("amendment_date = %v", am.Properties["amendment_date"])
	}
	if _, ok := am.Properties["target_provision_ref"]; ok {
		t.Fatalf("target_provision_ref should be omitted for law-level amendment")
	}
}

func TestHasAmendmentEdgeResolves(t *testing.T) {
	docs := []LovDoc{{
		RefID:             "lov/2020-01-01-1",
		DocType:           "Law",
		Title:             "Testloven",
		LastChangedByRef:  "lov/2023-06-01-2",
		LastChangeInForce: "2023-06-01",
	}}
	// Both the amended law and the Amendment object exist in objectKeys.
	objs := buildSeedObjectRecords(docs, nil, nil, nil)
	keys := map[string]bool{}
	for _, o := range objs {
		keys[o.Key] = true
	}
	rels := buildSeedRelationshipRecords(docs, nil, keys)
	found := false
	for _, r := range rels {
		if r.Type == "HAS_AMENDMENT" {
			found = true
			if r.SrcKey != "lov/2020-01-01-1" {
				t.Fatalf("HAS_AMENDMENT SrcKey = %q; want law ref", r.SrcKey)
			}
			if !keys[r.DstKey] {
				t.Fatalf("HAS_AMENDMENT DstKey %q not in objectKeys", r.DstKey)
			}
		}
	}
	if !found {
		t.Fatalf("no HAS_AMENDMENT edge emitted")
	}
}

func TestKeepAmendment(t *testing.T) {
	targets := map[string]bool{"lov/2020-01-01-1": true}
	cases := []struct {
		name string
		rec  amendmentRecord
		want bool
	}{
		{"metadata in corpus", amendmentRecord{TargetLawRefID: "lov/2020-01-01-1", Source: "metadata"}, true},
		{"target not in corpus", amendmentRecord{TargetLawRefID: "lov/1999-01-01-9", Source: "metadata"}, false},
		{"body_note with date", amendmentRecord{TargetLawRefID: "lov/2020-01-01-1", Source: "body_note", AmendmentDate: "2024-01-01"}, true},
		{"body_note with ref only", amendmentRecord{TargetLawRefID: "lov/2020-01-01-1", Source: "body_note", AmendedByRefID: "LOV-2024-01-01-1"}, true},
		{"body_note junk (both empty)", amendmentRecord{TargetLawRefID: "lov/2020-01-01-1", Source: "body_note"}, false},
	}
	for _, tc := range cases {
		if got := keepAmendment(tc.rec, targets); got != tc.want {
			t.Fatalf("keepAmendment(%s) = %v; want %v", tc.name, got, tc.want)
		}
	}
}

func TestAmendmentDanglingTargetFiltered(t *testing.T) {
	// An AmendsRefs target not in the corpus must not produce an Amendment
	// object or a HAS_AMENDMENT edge.
	docs := []LovDoc{{
		RefID:      "lov/2020-01-01-1",
		DocType:    "Law",
		Title:      "Testloven",
		AmendsRefs: []string{"lov/1999-01-01-9"}, // not in corpus
	}}
	objs := buildSeedObjectRecords(docs, nil, nil, nil)
	for _, o := range objs {
		if o.Type == "Amendment" {
			t.Fatalf("dangling-target doc produced Amendment object: %+v", o)
		}
	}
	rels := buildSeedRelationshipRecords(docs, nil, map[string]bool{"lov/2020-01-01-1": true})
	for _, r := range rels {
		if r.Type == "HAS_AMENDMENT" {
			t.Fatalf("dangling-target doc produced HAS_AMENDMENT edge: %+v", r)
		}
	}
}

func TestAmendmentBodyNoteJunkFiltered(t *testing.T) {
	// A body_note match with no amending ref and no date ("Ny lydelse" without a
	// "ved lov … nr. N" suffix) must not be emitted as Amendment or
	// ProvisionVersion.
	docs := []LovDoc{{
		RefID:   "lov/2020-01-01-1",
		DocType: "Law",
		Title:   "Testloven",
		Paragraphs: []LovParagraph{
			{SectionID: "kapittel-1-paragraf-2", ParagraphNum: "§ 8-10", Content: "Ny lydelse skal lyde:"},
		},
	}}
	objs := buildSeedObjectRecords(docs, nil, nil, nil)
	for _, o := range objs {
		if o.Type == "Amendment" || o.Type == "ProvisionVersion" {
			t.Fatalf("junk body_note produced %s object: %+v", o.Type, o)
		}
	}
	rels := buildSeedRelationshipRecords(docs, nil, map[string]bool{"lov/2020-01-01-1": true})
	for _, r := range rels {
		if r.Type == "HAS_AMENDMENT" || r.Type == "HAS_VERSION" {
			t.Fatalf("junk body_note produced %s edge", r.Type)
		}
	}
}
