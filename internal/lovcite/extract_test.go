package lovcite

import "testing"

func TestExtractActRefs(t *testing.T) {
	refs := ExtractActRefs("Jf. lov/2005-06-17-62 og forskrift/2006-02-17-204 § 4.")
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}
	if refs[0].Raw != "lov/2005-06-17-62" || refs[0].Kind != "lov" {
		t.Errorf("refs[0] = %+v", refs[0])
	}
	if refs[1].Raw != "forskrift/2006-02-17-204" || refs[1].Kind != "forskrift" {
		t.Errorf("refs[1] = %+v", refs[1])
	}
}

func TestExtractActRefsHistoric(t *testing.T) {
	refs := ExtractActRefs("Se lov/1687-04-15.")
	if len(refs) != 1 || refs[0].Raw != "lov/1687-04-15" {
		t.Fatalf("got %+v", refs)
	}
}

func TestExtractSectionRefs(t *testing.T) {
	refs := ExtractSectionRefs("Se § 1-2 og § 9-9 første ledd.")
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2: %+v", len(refs), refs)
	}
	if refs[0].Token != "1-2" || refs[1].Token != "9-9" {
		t.Errorf("tokens = %q, %q", refs[0].Token, refs[1].Token)
	}
}

func TestExtractSectionRefsFlat(t *testing.T) {
	refs := ExtractSectionRefs("Etter § 6 og § 7.")
	if len(refs) != 2 || refs[0].Token != "6" || refs[1].Token != "7" {
		t.Fatalf("got %+v", refs)
	}
}

func TestExtractSectionRefsSkipsPlural(t *testing.T) {
	refs := ExtractSectionRefs("Jf. §§ 40, 41 og 43.")
	// "§§" is a plural range/list marker and must not be counted.
	if len(refs) != 0 {
		t.Fatalf("got %d refs, want 0: %+v", len(refs), refs)
	}
}

func TestExtractSectionRefsAttachedLetter(t *testing.T) {
	refs := ExtractSectionRefs("Jf. § 1-3a.")
	if len(refs) != 1 || refs[0].Token != "1-3a" {
		t.Fatalf("got %+v", refs)
	}
}

func TestExtractSectionRefsDashVariants(t *testing.T) {
	for _, in := range []string{"§ 1-3", "§ 1–3", "§ 1—3"} {
		refs := ExtractSectionRefs(in)
		if len(refs) != 1 || refs[0].Token != "1-3" {
			t.Errorf("%q -> %+v, want token 1-3", in, refs)
		}
	}
}

func TestIsHeadingLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"### § 1-1 — Formål", true},
		{"# Tittel", true},
		{"  ### § 1-2", true},
		{"Se § 1-2.", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsHeadingLine(c.line); got != c.want {
			t.Errorf("IsHeadingLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestAttributeSectionRefsOwning(t *testing.T) {
	actRefs := []ActRef{}
	secRefs := []SectionRef{{Raw: "§ 1-2", Token: "1-2", Start: 0, End: 5}}
	got := attributeSectionRefs("Se § 1-2.", "lov/2005-06-17-62", actRefs, secRefs)
	if len(got) != 1 || got[0] != "lov/2005-06-17-62" {
		t.Fatalf("got %v", got)
	}
}

func TestAttributeSectionRefsCrossAct(t *testing.T) {
	line := "Jf. lov/1900-01-01-1 § 2."
	actRefs := ExtractActRefs(line)
	secRefs := ExtractSectionRefs(line)
	got := attributeSectionRefs(line, "lov/2005-06-17-62", actRefs, secRefs)
	if len(got) != 1 || got[0] != "lov/1900-01-01-1" {
		t.Fatalf("got %v, want lov/1900-01-01-1", got)
	}
}

func TestAttributeSectionRefsCrossActTooFar(t *testing.T) {
	// Act ref far away (> crossActWindow bytes) from the § ref: stays owning.
	line := "Jf. lov/1900-01-01-1" + string(make([]byte, crossActWindow+10)) + " § 2."
	actRefs := ExtractActRefs(line)
	secRefs := ExtractSectionRefs(line)
	got := attributeSectionRefs(line, "lov/2005-06-17-62", actRefs, secRefs)
	if len(got) != 1 || got[0] != "lov/2005-06-17-62" {
		t.Fatalf("got %v, want owning doc", got)
	}
}
