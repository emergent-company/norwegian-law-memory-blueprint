package lovcheck

import "testing"

func TestCheckCleanCorpusHasZeroViolations(t *testing.T) {
	r := runCheck(t, baseObjects(), baseRelationships(), "", "")
	if r.TotalViolations != 0 {
		t.Fatalf("expected 0 violations, got %d: %+v", r.TotalViolations, r)
	}
	for _, c := range r.Checks {
		if c.Count != 0 {
			t.Errorf("%s: expected 0, got %d", c.ID, c.Count)
		}
	}
}

func TestV1DuplicateObjectKey(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl": `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n" +
			`{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n",
	}
	r := runCheck(t, objects, map[string]string{}, "", "")
	if got := countFor(r, "V1"); got != 1 {
		t.Fatalf("V1 = %d, want 1", got)
	}
}

func TestV2DanglingEndpoint(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl": `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n",
	}
	rels := map[string]string{
		"REFERENCES.jsonl": `{"type":"REFERENCES","srcKey":"lov/2005-06-17-62","dstKey":"lov/1999-99-99-9","properties":{}}` + "\n",
	}
	r := runCheck(t, objects, rels, "", "")
	if got := countFor(r, "V2"); got != 1 {
		t.Fatalf("V2 = %d, want 1", got)
	}
}

func TestV3SelfLoop(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl": `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n",
	}
	rels := map[string]string{
		"REFERENCES.jsonl": `{"type":"REFERENCES","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62","properties":{}}` + "\n",
	}
	r := runCheck(t, objects, rels, "", "")
	if got := countFor(r, "V3"); got != 1 {
		t.Fatalf("V3 = %d, want 1", got)
	}
}

func TestV4KeyMismatch(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl":            `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n",
		"LegalParagraph.jsonl": `{"type":"LegalParagraph","key":"WRONG-KEY","properties":{"law_ref_id":"lov/2005-06-17-62","section_id":"kapittel-1-paragraf-1"}}` + "\n",
	}
	r := runCheck(t, objects, map[string]string{}, "", "")
	if got := countFor(r, "V4"); got != 1 {
		t.Fatalf("V4 = %d, want 1", got)
	}
}

func TestV4EmptyFieldsAndDanglingParent(t *testing.T) {
	objects := map[string]string{
		"LegalParagraph.jsonl": `{"type":"LegalParagraph","key":"a#b","properties":{"law_ref_id":"","section_id":"b"}}` + "\n" +
			`{"type":"LegalParagraph","key":"c#d","properties":{"law_ref_id":"lov/1999-99-99-9","section_id":"d"}}` + "\n",
	}
	r := runCheck(t, objects, map[string]string{}, "", "")
	// First: empty law_ref_id. Second: law_ref_id not a Law/Regulation object.
	if got := countFor(r, "V4"); got != 2 {
		t.Fatalf("V4 = %d, want 2", got)
	}
}

func TestV4EmptySectionID(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl":            `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n",
		"LegalParagraph.jsonl": `{"type":"LegalParagraph","key":"lov/2005-06-17-62#","properties":{"law_ref_id":"lov/2005-06-17-62","section_id":""}}` + "\n",
	}
	r := runCheck(t, objects, map[string]string{}, "", "")
	if got := countFor(r, "V4"); got != 1 {
		t.Fatalf("V4 = %d, want 1", got)
	}
}

func TestV5OrphanParagraph(t *testing.T) {
	objects := baseObjects()
	r := runCheck(t, objects, map[string]string{}, "", "") // no HAS_PARAGRAPH at all
	if got := countFor(r, "V5"); got != 1 {
		t.Fatalf("V5 = %d, want 1", got)
	}
}

func TestV5DstNotLegalParagraph(t *testing.T) {
	objects := baseObjects()
	rels := map[string]string{
		"HAS_PARAGRAPH.jsonl": `{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62","properties":{"position":1}}` + "\n" +
			// A correct edge so the LegalParagraph is not also flagged as orphan.
			`{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"position":2}}` + "\n",
	}
	r := runCheck(t, objects, rels, "", "")
	// Only the first edge's dst (a Law, not a LegalParagraph) is a violation.
	if got := countFor(r, "V5"); got != 1 {
		t.Fatalf("V5 = %d, want 1", got)
	}
}

func TestV5SrcMismatch(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl":            `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n",
		"LegalParagraph.jsonl": `{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"law_ref_id":"lov/2005-06-17-62","section_id":"kapittel-1-paragraf-1"}}` + "\n",
	}
	rels := map[string]string{
		"HAS_PARAGRAPH.jsonl": `{"type":"HAS_PARAGRAPH","srcKey":"lov/1999-99-99-9","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"position":1}}` + "\n",
	}
	r := runCheck(t, objects, rels, "", "")
	if got := countFor(r, "V5"); got != 1 {
		t.Fatalf("V5 = %d, want 1", got)
	}
}

func TestV6RefIDPattern(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl": `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"bad-ref"}}` + "\n" +
			`{"type":"Law","key":"lov/2006-06-18-63","properties":{}}` + "\n",
	}
	r := runCheck(t, objects, map[string]string{}, "", "")
	// First: ref_id not matching. Second: ref_id empty.
	if got := countFor(r, "V6"); got != 2 {
		t.Fatalf("V6 = %d, want 2", got)
	}
}

func TestV6RefIDHistoricAllowed(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl": `{"type":"Law","key":"lov/1687-04-15","properties":{"ref_id":"lov/1687-04-15"}}` + "\n",
	}
	r := runCheck(t, objects, map[string]string{}, "", "")
	if got := countFor(r, "V6"); got != 0 {
		t.Fatalf("V6 = %d, want 0 (historic no-suffix ref is valid)", got)
	}
}

func TestV7DateSanity(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl": `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62","date_of_publication":"2005-13-45"}}` + "\n" +
			`{"type":"Law","key":"lov/2006-06-18-63","properties":{"ref_id":"lov/2006-06-18-63","date_in_force":"2999-01-01"}}` + "\n" +
			`{"type":"Law","key":"lov/2007-06-19-64","properties":{"ref_id":"lov/2007-06-19-64","date_of_publication":"not-a-date"}}` + "\n",
	}
	r := runCheck(t, objects, map[string]string{}, "", "")
	// 2005-13-45 (invalid month), 2999-01-01 (beyond maxYear 2031), not-a-date.
	if got := countFor(r, "V7"); got != 3 {
		t.Fatalf("V7 = %d, want 3", got)
	}
}

func TestV8ManifestCrossCheck(t *testing.T) {
	objects := baseObjects()
	rels := baseRelationships()
	manifest := `{"counts":{"objects":999,"relationships":1,"by_type":{"Law":1,"LegalParagraph":1}}}` + "\n"
	r := runCheck(t, objects, rels, manifest, "")
	// Actual: 2 objects (Law + LegalParagraph), 1 relationship.
	// Mismatches: objects (999 vs 2), relationships (1 vs 1 = match), and
	// by_type Law (1 vs 1 match), LegalParagraph (1 vs 1 match) -> only objects.
	if got := countFor(r, "V8"); got != 1 {
		t.Fatalf("V8 = %d, want 1", got)
	}
}

func TestV8ManifestMissingFileSkipped(t *testing.T) {
	r := runCheck(t, baseObjects(), baseRelationships(), "", "")
	if countFor(r, "V8") != 0 {
		t.Fatalf("V8 should be 0 when manifest absent")
	}
	found := false
	for _, n := range r.Notes {
		if len(n) > 0 && n[:4] == "V8 s" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a V8-skipped note, got %+v", r.Notes)
	}
}

func TestInventoryDeclaredButUnused(t *testing.T) {
	objects := baseObjects()
	rels := baseRelationships()
	schema := "name: t\nobjectTypes:\n  - name: Law\n  - name: Regulation\nrelationshipTypes:\n  - name: HAS_PARAGRAPH\n  - name: EU_CITES\n"
	r := runCheck(t, objects, rels, "", schema)
	// EU_CITES declared but never emitted; Regulation declared but absent.
	unused := r.Inventory.DeclaredButUnusedRelationships
	if len(unused) != 1 || unused[0] != "EU_CITES" {
		t.Errorf("declared-but-unused relationships = %v, want [EU_CITES]", unused)
	}
	absent := r.Inventory.DeclaredButAbsentObjectTypes
	if len(absent) != 1 || absent[0] != "Regulation" {
		t.Errorf("declared-but-absent object types = %v, want [Regulation]", absent)
	}
}

func TestCheckDeterministic(t *testing.T) {
	objects := map[string]string{
		"Law.jsonl": `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n" +
			`{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n",
	}
	rels := map[string]string{
		"REFERENCES.jsonl": `{"type":"REFERENCES","srcKey":"lov/2005-06-17-62","dstKey":"lov/1999-99-99-9","properties":{}}` + "\n" +
			`{"type":"REFERENCES","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62","properties":{}}` + "\n",
	}
	a := runCheck(t, objects, rels, "", "")
	b := runCheck(t, objects, rels, "", "")
	if a.TotalViolations != b.TotalViolations {
		t.Fatalf("non-deterministic totals: %d vs %d", a.TotalViolations, b.TotalViolations)
	}
	for i := range a.Checks {
		if a.Checks[i].ID != b.Checks[i].ID || a.Checks[i].Count != b.Checks[i].Count ||
			len(a.Checks[i].Samples) != len(b.Checks[i].Samples) {
			t.Errorf("check %s differs between runs", a.Checks[i].ID)
		}
		for j := range a.Checks[i].Samples {
			if a.Checks[i].Samples[j] != b.Checks[i].Samples[j] {
				t.Errorf("check %s sample %d differs", a.Checks[i].ID, j)
			}
		}
	}
}

func TestClassifyDate(t *testing.T) {
	cases := []struct {
		in      string
		maxYear int
		ok      bool
	}{
		{"2005-06-17", 2031, true},
		{"1687-04-15", 2031, true},
		{"2026-02-29", 2031, false}, // 2026 is not a leap year
		{"2020-02-29", 2031, true},  // 2020 is a leap year
		{"2005-13-01", 2031, false}, // bad month
		{"2005-01-32", 2031, false}, // bad day
		{"2005-1-1", 2031, false},   // not zero-padded
		{"2999-01-01", 2031, false}, // beyond threshold
		{"", 2031, false},
		{"not-a-date", 2031, false},
	}
	for _, c := range cases {
		if ok, _ := classifyDate(c.in, c.maxYear); ok != c.ok {
			t.Errorf("classifyDate(%q, %d) = %v, want %v", c.in, c.maxYear, ok, c.ok)
		}
	}
}
