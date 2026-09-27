package main

import (
	"reflect"
	"testing"
)

// sparqlFixture mimics the CELLAR all-metadata response for CELEX 32003L0004,
// with cartesian duplication across the multivalued attributes (authors,
// subjects, legal bases, eurovoc).
const sparqlFixture = `{
  "head": { "vars": ["title","dateDocument","dateEffect","resourceTypeLabel","authorLabel","dgLabel","subjectLabel","directoryLabel","legalBasisCelex","docId","eurovoc","procedureRef","citedCelex","amenderCelex"] },
  "results": { "bindings": [
    {"title":{"value":"Directive 2003/4/EC of the European Parliament and of the Council of 28 January 2003 on public access to environmental information and repealing Council Directive 90/313/EEC"},"dateDocument":{"value":"2003-01-28"},"dateEffect":{"value":"2003-02-14"},"resourceTypeLabel":{"value":"Directive"},"authorLabel":{"value":"Council of the European Union"},"dgLabel":{"value":"Directorate-General for Environment"},"subjectLabel":{"value":"Environment"},"directoryLabel":{"value":"Consumer information, education and representation"},"legalBasisCelex":{"value":"11997E175"},"docId":{"value":"oj:JOL_2003_041_R_0026_01"},"eurovoc":{"value":"http://eurovoc.europa.eu/2470"},"procedureRef":{"value":"2000/0169/COD"},"citedCelex":{"value":"31995L0046"}},
    {"title":{"value":"Directive 2003/4/EC of the European Parliament and of the Council of 28 January 2003 on public access to environmental information and repealing Council Directive 90/313/EEC"},"dateDocument":{"value":"2003-01-28"},"dateEffect":{"value":"2003-02-14"},"resourceTypeLabel":{"value":"Directive"},"authorLabel":{"value":"European Parliament"},"dgLabel":{"value":"Directorate-General for Environment"},"subjectLabel":{"value":"Internal market - Principles"},"directoryLabel":{"value":"Consumer information, education and representation"},"legalBasisCelex":{"value":"11997E175"},"docId":{"value":"oj:JOL_2003_041_R_0026_01"},"eurovoc":{"value":"http://eurovoc.europa.eu/441"},"procedureRef":{"value":"2000/0169/COD"},"citedCelex":{"value":"31995L0046"}},
    {"title":{"value":"Directive 2003/4/EC of the European Parliament and of the Council of 28 January 2003 on public access to environmental information and repealing Council Directive 90/313/EEC"},"dateDocument":{"value":"2003-01-28"},"dateEffect":{"value":"2003-02-14"},"resourceTypeLabel":{"value":"Directive"},"authorLabel":{"value":"European Parliament"},"dgLabel":{"value":"Directorate-General for Environment"},"subjectLabel":{"value":"Approximation of laws"},"directoryLabel":{"value":"Consumer information, education and representation"},"legalBasisCelex":{"value":"11997E251"},"docId":{"value":"oj:JOL_2003_041_R_0026_01"},"eurovoc":{"value":"http://eurovoc.europa.eu/513"},"procedureRef":{"value":"2000/0169/COD"},"citedCelex":{"value":"31995L0046"}},
    {"title":{"value":"Directive 2003/4/EC of the European Parliament and of the Council of 28 January 2003 on public access to environmental information and repealing Council Directive 90/313/EEC"},"dateDocument":{"value":"2003-01-28"},"dateEffect":{"value":"2003-02-14"},"resourceTypeLabel":{"value":"Directive"},"authorLabel":{"value":"European Parliament"},"dgLabel":{"value":"Directorate-General for Environment"},"subjectLabel":{"value":"Environment"},"directoryLabel":{"value":"Consumer information, education and representation"},"legalBasisCelex":{"value":"11997E251"},"docId":{"value":"oj:JOL_2003_041_R_0026_01"},"eurovoc":{"value":"http://eurovoc.europa.eu/5399"},"procedureRef":{"value":"2000/0169/COD"},"citedCelex":{"value":"31995L0046"}},
    {"title":{"value":"Directive 2003/4/EC of the European Parliament and of the Council of 28 January 2003 on public access to environmental information and repealing Council Directive 90/313/EEC"},"dateDocument":{"value":"2003-01-28"},"dateEffect":{"value":"2003-02-14"},"resourceTypeLabel":{"value":"Directive"},"authorLabel":{"value":"European Parliament"},"dgLabel":{"value":"Directorate-General for Environment"},"subjectLabel":{"value":"Environment"},"directoryLabel":{"value":"Consumer information, education and representation"},"legalBasisCelex":{"value":"11997E175"},"docId":{"value":"oj:JOL_2003_041_R_0026_01"},"eurovoc":{"value":"http://eurovoc.europa.eu/2470"},"procedureRef":{"value":"2000/0169/COD"},"citedCelex":{"value":"31995l0046"}}
  ]}
}`

func TestParseSPARQLBindings(t *testing.T) {
	rows, err := parseSPARQLBindings([]byte(sparqlFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d; want 5", len(rows))
	}
	// A lowercased citedCelex ("31995l0046") must be uppercase-normalised later.
}

func TestCollapseDirectiveRows(t *testing.T) {
	rows, err := parseSPARQLBindings([]byte(sparqlFixture))
	if err != nil {
		t.Fatal(err)
	}
	d := collapseDirectiveRows(rows)

	if d.FullTitle != "Directive 2003/4/EC of the European Parliament and of the Council of 28 January 2003 on public access to environmental information and repealing Council Directive 90/313/EEC" {
		t.Fatalf("FullTitle = %q", d.FullTitle)
	}
	if d.Form != "Directive" {
		t.Fatalf("Form = %q", d.Form)
	}
	if d.DateOfDocument != "2003-01-28" || d.DateOfEffect != "2003-02-14" {
		t.Fatalf("dates = %q/%q", d.DateOfDocument, d.DateOfEffect)
	}
	if d.ResponsibleDG != "Directorate-General for Environment" {
		t.Fatalf("ResponsibleDG = %q", d.ResponsibleDG)
	}
	if d.DirectoryCode != "Consumer information, education and representation" {
		t.Fatalf("DirectoryCode = %q", d.DirectoryCode)
	}
	if d.ProcedureNum != "2000/0169/COD" {
		t.Fatalf("ProcedureNum = %q", d.ProcedureNum)
	}
	if d.OJReference != "oj:JOL_2003_041_R_0026_01" {
		t.Fatalf("OJReference = %q", d.OJReference)
	}

	// Multivalued fields: deduped + sorted.
	if !reflect.DeepEqual(d.Author, []string{"Council of the European Union", "European Parliament"}) {
		t.Fatalf("Author = %v", d.Author)
	}
	if !reflect.DeepEqual(d.SubjectMatter, []string{"Approximation of laws", "Environment", "Internal market - Principles"}) {
		t.Fatalf("SubjectMatter = %v", d.SubjectMatter)
	}
	if !reflect.DeepEqual(d.LegalBasis, []string{"11997E175", "11997E251"}) {
		t.Fatalf("LegalBasis = %v", d.LegalBasis)
	}
	if !reflect.DeepEqual(d.EuroVocIDs, []string{"2470", "441", "513", "5399"}) {
		t.Fatalf("EuroVocIDs = %v", d.EuroVocIDs)
	}
	// citedCelex is present in two casings (31995L0046 / 31995l0046); the
	// uppercase-normalised, deduped result must contain exactly one.
	if !reflect.DeepEqual(d.CitedCELEX, []string{"31995L0046"}) {
		t.Fatalf("CitedCELEX = %v", d.CitedCELEX)
	}
	if len(d.ModifiedByCELEX) != 0 {
		t.Fatalf("ModifiedByCELEX = %v; want empty (act never amended)", d.ModifiedByCELEX)
	}
}

func TestOJRefDecode(t *testing.T) {
	series, issue, year, page, ok := ojRefFromDocID("oj:JOL_2003_041_R_0026_01")
	if !ok || series != "L" || issue != "41" || year != "2003" || page != "26" {
		t.Fatalf("decode = %q/%q/%q/%q ok=%v", series, issue, year, page, ok)
	}
	if got := fmtOJDate("2003-02-14"); got != "14.2.2003" {
		t.Fatalf("fmtOJDate = %q", got)
	}
	if got := assembleOJReference("oj:JOL_2003_041_R_0026_01", "2003-02-14"); got != "OJ L 41, 14.2.2003, p. 26" {
		t.Fatalf("assembleOJReference = %q", got)
	}
	if got := assembleOJReference("oj:JOL_2003_041_R_0026_01", ""); got != "OJ L 41, 2003, p. 26" {
		t.Fatalf("assembleOJReference (no date) = %q", got)
	}
}

func TestCollectEUStubs(t *testing.T) {
	dirs := []*EUDirective{
		{CelexID: "32003L0004", CitedCELEX: []string{"31995L0046", "31990L0313"}},
		{CelexID: "31990L0313", ModifiedByCELEX: []string{"32015L0849"}},
	}
	stubs := collectEUStubs(dirs)
	// "31995L0046" is cited and not among dirs → stub; "31990L0313" IS among dirs
	// → no stub; "32015L0849" amended and not among dirs → stub.
	var got []string
	for _, s := range stubs {
		got = append(got, s.CelexID)
	}
	if !reflect.DeepEqual(got, []string{"31995L0046", "32015L0849"}) {
		t.Fatalf("stubs = %v", got)
	}
}

func TestCollectEUStubsExistingTargetNoDangling(t *testing.T) {
	// When the cited CELEX is itself a fetched directive, no stub is created and
	// the EU_CITES edge would resolve to the real object.
	dirs := []*EUDirective{
		{CelexID: "32003L0004", CitedCELEX: []string{"31990L0313"}},
		{CelexID: "31990L0313"},
	}
	if stubs := collectEUStubs(dirs); len(stubs) != 0 {
		t.Fatalf("stubs = %v; want none (target exists)", stubs)
	}
}
