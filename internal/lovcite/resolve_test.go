package lovcite

import "testing"

func TestResolveRefParagraphForm(t *testing.T) {
	idx := buildStandard(t, false)

	// §-form with no space.
	res := idx.ResolveRef("lov/2005-06-17-62#§1-1")
	if !res.ActFound || len(res.Targets) != 1 {
		t.Fatalf("got %+v", res)
	}
	if res.Targets[0].SectionID != "kapittel-1-paragraf-1" {
		t.Errorf("section_id = %q", res.Targets[0].SectionID)
	}

	// §-form with whitespace tolerated.
	res = idx.ResolveRef("lov/2005-06-17-62#§ 1-1")
	if len(res.Targets) != 1 {
		t.Fatalf("got %d targets", len(res.Targets))
	}
}

func TestResolveRefSectionIDForm(t *testing.T) {
	idx := buildStandard(t, false)
	res := idx.ResolveRef("lov/2005-06-17-62#kapittel-1-paragraf-1")
	if !res.ActFound || len(res.Targets) != 1 {
		t.Fatalf("got %+v", res)
	}
	if res.Targets[0].ParagraphNum != "§ 1-1" {
		t.Errorf("paragraph_num = %q", res.Targets[0].ParagraphNum)
	}
}

func TestResolveRefBareAct(t *testing.T) {
	idx := buildStandard(t, false)
	res := idx.ResolveRef("lov/1900-01-01-1")
	if !res.ActFound || len(res.Targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(res.Targets))
	}
	// Deterministic: sorted by key.
	if res.Targets[0].SectionID != "paragraf-1" || res.Targets[1].SectionID != "paragraf-2" {
		t.Errorf("target order = %q, %q", res.Targets[0].SectionID, res.Targets[1].SectionID)
	}
}

func TestResolveRefMissingAct(t *testing.T) {
	idx := buildStandard(t, false)
	res := idx.ResolveRef("lov/1999-99-99-9#§1-1")
	if res.ActFound {
		t.Fatal("ActFound should be false")
	}
	if len(res.Targets) != 0 {
		t.Fatal("no targets expected")
	}
}

func TestResolveRefMissingSection(t *testing.T) {
	idx := buildStandard(t, false)
	res := idx.ResolveRef("lov/2005-06-17-62#§9-9")
	if !res.ActFound {
		t.Fatal("act should be found")
	}
	if len(res.Targets) != 0 {
		t.Fatal("section should be unresolved")
	}
}

func TestResolveRefRegulation(t *testing.T) {
	idx := buildStandard(t, false)
	// No regulation in the fixture, so this act is unknown.
	res := idx.ResolveRef("forskrift/2006-02-17-204#§1-1")
	if res.ActFound {
		t.Fatal("regulation not in fixture")
	}
}
