package evalgen

import (
	"strings"
	"testing"
)

func TestParagraphRef(t *testing.T) {
	cases := []struct {
		name         string
		lawKey       string
		paragraphNum string
		sectionID    string
		want         string
	}{
		{"number form", "lov/2005-06-17-62", "§ 1-1", "kapittel-1-paragraf-1", "lov/2005-06-17-62#§1-1"},
		{"number form no space", "lov/2005-06-17-62", "§1-1", "kapittel-1-paragraf-1", "lov/2005-06-17-62#§1-1"},
		{"letter suffix", "lov/2005-06-17-62", "§ 1-1 a", "kapittel-1-paragraf-1", "lov/2005-06-17-62#§1-1 a"},
		{"old law art number", "lov/1687-04-15", "15 Art", "kapittel-3-kapittel-1-paragraf-1", "lov/1687-04-15#§15 Art"},
		{"empty num falls back to section id", "lov/2005-06-17-62", "", "ledd-1", "lov/2005-06-17-62#ledd-1"},
		{"empty key", "", "§ 1", "paragraf-1", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := paragraphRef(c.lawKey, c.paragraphNum, c.sectionID); got != c.want {
				t.Fatalf("paragraphRef() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestStripHeading(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"### § 1-1 — Lovens formål\n\nLovens formål er å sikre trygge forhold.", "Lovens formål er å sikre trygge forhold."},
		{"### § 1\n\nBody text", "Body text"},
		{"No heading here", "No heading here"},
		{"### heading only", ""},
	}
	for _, c := range cases {
		if got := stripHeading(c.in); got != c.want {
			t.Fatalf("stripHeading(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCapText(t *testing.T) {
	if got := capText("abc", 10); got != "abc" {
		t.Fatalf("capText short = %q", got)
	}
	long := strings.Repeat("x", 100)
	got := capText(long, 5)
	if got != "xxxxx…" {
		t.Fatalf("capText long = %q, want %q", got, "xxxxx…")
	}
}

func TestProvisionGoldPoint(t *testing.T) {
	in := "### § 1-1 — Lovens formål\n\nLovens formål er å sikre trygge forhold.\n\nAndre setning."
	got := provisionGoldPoint(in)
	want := "Lovens formål er å sikre trygge forhold. Andre setning."
	if got != want {
		t.Fatalf("provisionGoldPoint = %q, want %q", got, want)
	}
}

func TestActLabel(t *testing.T) {
	got := actLabel(actMeta{ShortTitle: "Arbeidsmiljøloven – aml", Name: "Lov om arbeidsmiljø"}, "lov/x")
	if got != "Arbeidsmiljøloven – aml" {
		t.Fatalf("actLabel short_title = %q", got)
	}
	got = actLabel(actMeta{ShortTitle: "", Name: "Lov om testing"}, "lov/x")
	if got != "Lov om testing" {
		t.Fatalf("actLabel name fallback = %q", got)
	}
	got = actLabel(actMeta{}, "lov/x")
	if got != "lov/x" {
		t.Fatalf("actLabel key fallback = %q", got)
	}
}

func TestSectionTitle(t *testing.T) {
	got := sectionTitle(paraMeta{Title: "Lovens formål", SectionLabel: "§ 1-1"})
	if got != "Lovens formål" {
		t.Fatalf("sectionTitle title = %q", got)
	}
	got = sectionTitle(paraMeta{SectionLabel: "15 Art"})
	if got != "15 Art" {
		t.Fatalf("sectionTitle fallback = %q", got)
	}
}

func TestQuestions(t *testing.T) {
	if got := syntheticQuestion("Arbeidsmiljøloven – aml", "§ 1-1"); got != "Hva bestemmer Arbeidsmiljøloven – aml § 1-1?" {
		t.Fatalf("syntheticQuestion = %q", got)
	}
	if got := retrievalQuestion("Arbeidsmiljøloven – aml", "Lovens formål"); got != "Hvilken bestemmelse i Arbeidsmiljøloven – aml regulerer Lovens formål?" {
		t.Fatalf("retrievalQuestion = %q", got)
	}
}

func TestSplitAnswerable(t *testing.T) {
	cases := []struct {
		limit    int
		ratio    float64
		wantAns  int
		wantUnan int
	}{
		{20, 0.2, 16, 4},
		{10, 0.0, 10, 0},
		{10, 1.0, 0, 10},
		{5, 0.5, 2, 3}, // math.Round(2.5)=3, so 2 answerable
		{0, 0.2, 0, 0},
		{7, 1.5, 0, 7}, // clamped
	}
	for _, c := range cases {
		a, u := splitAnswerable(c.limit, c.ratio)
		if a != c.wantAns || u != c.wantUnan {
			t.Fatalf("splitAnswerable(%d, %v) = (%d, %d), want (%d, %d)", c.limit, c.ratio, a, u, c.wantAns, c.wantUnan)
		}
	}
}
