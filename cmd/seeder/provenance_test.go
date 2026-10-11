package main

import "testing"

// pinRetrievedAt pins the run-date function for the duration of a test so the
// `retrieved_at` assertion is stable, restoring it afterward.
func pinRetrievedAt(t *testing.T, date string) {
	t.Helper()
	orig := retrievedAtFunc
	retrievedAtFunc = func() string { return date }
	t.Cleanup(func() { retrievedAtFunc = orig })
}

func TestProvenanceFieldsLaw(t *testing.T) {
	pinRetrievedAt(t, "2026-10-11")

	docs := []LovDoc{{RefID: "lov/2020-01-01-1", DocType: "Law", Title: "Testloven"}}
	objs := buildSeedObjectRecords(docs, nil, nil, nil)

	var law *seedObjectRecord
	for i := range objs {
		if objs[i].Type == "Law" {
			law = &objs[i]
			break
		}
	}
	if law == nil {
		t.Fatalf("no Law object emitted")
	}
	if got := law.Properties["publisher"]; got != "Lovdata" {
		t.Fatalf("publisher = %v; want Lovdata", got)
	}
	if got := law.Properties["license"]; got != "NLOD-2.0" {
		t.Fatalf("license = %v; want NLOD-2.0", got)
	}
	if got := law.Properties["source_url"]; got != "https://lovdata.no/dokument/NL/lov/2020-01-01-1" {
		t.Fatalf("source_url = %v", got)
	}
	if got := law.Properties["retrieved_at"]; got != "2026-10-11" {
		t.Fatalf("retrieved_at = %v; want 2026-10-11", got)
	}
}

func TestProvenanceFieldsRegulation(t *testing.T) {
	pinRetrievedAt(t, "2026-10-11")

	docs := []LovDoc{{RefID: "forskrift/2009-12-18-1585", DocType: "Regulation", Title: "Testforskrift"}}
	objs := buildSeedObjectRecords(docs, nil, nil, nil)

	var reg *seedObjectRecord
	for i := range objs {
		if objs[i].Type == "Regulation" {
			reg = &objs[i]
			break
		}
	}
	if reg == nil {
		t.Fatalf("no Regulation object emitted")
	}
	if got := reg.Properties["publisher"]; got != "Lovdata" {
		t.Fatalf("publisher = %v", got)
	}
	if got := reg.Properties["license"]; got != "NLOD-2.0" {
		t.Fatalf("license = %v", got)
	}
	if got := reg.Properties["source_url"]; got != "https://lovdata.no/dokument/NL/forskrift/2009-12-18-1585" {
		t.Fatalf("source_url = %v", got)
	}
}

func TestProvenanceFieldsEUDirective(t *testing.T) {
	pinRetrievedAt(t, "2026-10-11")

	dirs := []*EUDirective{{
		CelexID:    "32014L0026",
		ShortTitle: "Directive 2014/26/EU",
		Community:  "EU",
		URL:        "https://eur-lex.europa.eu/eli/dir/2014/26",
		InForce:    true,
	}}
	objs := buildSeedObjectRecords(nil, dirs, nil, nil)

	var eu *seedObjectRecord
	for i := range objs {
		if objs[i].Type == "EUDirective" {
			eu = &objs[i]
			break
		}
	}
	if eu == nil {
		t.Fatalf("no EUDirective object emitted")
	}
	if got := eu.Properties["publisher"]; got != "Publications Office of the European Union" {
		t.Fatalf("publisher = %v", got)
	}
	if got := eu.Properties["license"]; got != "EUR-Lex reuse" {
		t.Fatalf("license = %v", got)
	}
	if got := eu.Properties["source_url"]; got != "https://eur-lex.europa.eu/eli/dir/2014/26" {
		t.Fatalf("source_url = %v", got)
	}
	if got := eu.Properties["retrieved_at"]; got != "2026-10-11" {
		t.Fatalf("retrieved_at = %v", got)
	}
}

func TestProvenanceFieldsEUStub(t *testing.T) {
	pinRetrievedAt(t, "2026-10-11")

	// A cited CELEX not among the directives produces a stub, which must also
	// carry provenance (with source_url derived from CELEX).
	dirs := []*EUDirective{{
		CelexID:    "32014L0026",
		ShortTitle: "Directive 2014/26/EU",
		Community:  "EU",
		URL:        "https://eur-lex.europa.eu/eli/dir/2014/26",
		InForce:    true,
		CitedCELEX: []string{"31995L0046"},
	}}
	objs := buildSeedObjectRecords(nil, dirs, nil, nil)

	var stub *seedObjectRecord
	for i := range objs {
		if objs[i].Type == "EUDirective" && objs[i].Key == "eu_31995L0046" {
			stub = &objs[i]
			break
		}
	}
	if stub == nil {
		t.Fatalf("no EUDirective stub emitted")
	}
	if got := stub.Properties["publisher"]; got != "Publications Office of the European Union" {
		t.Fatalf("stub publisher = %v", got)
	}
	if got := stub.Properties["license"]; got != "EUR-Lex reuse" {
		t.Fatalf("stub license = %v", got)
	}
	if got := stub.Properties["source_url"]; got != "https://eur-lex.europa.eu/eli/dir/1995/46" {
		t.Fatalf("stub source_url = %v", got)
	}
	if got := stub.Properties["retrieved_at"]; got != "2026-10-11" {
		t.Fatalf("stub retrieved_at = %v", got)
	}
}
