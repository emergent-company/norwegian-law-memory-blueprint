package main

import (
	"reflect"
	"testing"
)

func TestExtractEUCelexForms(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"directive with suffix", "EØS-avtalen vedlegg IX nr. 8 (direktiv 2009/103/EF)", []string{"32009L0103"}},
		{"directive EU suffix", "EØS-avtalen vedlegg II (direktiv 2014/40/EU)", []string{"32014L0040"}},
		{"directive 2-digit year", "EØS-avtalen vedlegg XIX nr. 7a (direktiv 93/13/EØF).", []string{"31993L0013"}},
		{"directive no suffix 2-digit", "EØS-avtalen vedlegg XVII nr. 1 (direktiv 87/54).", []string{"31987L0054"}},
		{"regulation number/year", "EØS-avtalen protokoll 28 (forordning (EF) nr. 469/2009)", []string{"32009R0469"}},
		{"regulation year/number", "EØS-avtalen vedlegg XIII forordning (EU) 2019/621.", []string{"32019R0621"}},
		{"regulation 2-digit year", "EØS-avtalen vedlegg XIII nr. 53 (forordning (EØF) nr. 4055/86)", []string{"31986R4055"}},
		{"decision", "EØS-avtalen vedlegg II kap. XIX nr. 3d (Europaparlaments- og rådsbeslutning nr. 768/2008/EF).", []string{"32008D0768"}},
		{"directive with (EU) prefix", "EØS-avtalen vedlegg XI nr. 5i (direktiv (EU) 2015/1535)", []string{"32015L1535"}},
		{"multiple refs", "EØS-avtalen vedlegg XX nr. 1ea (forordning (EF) nr. 1221/2009), nr. 1f (direktiv 2010/75/EU)", []string{"32009R1221", "32010L0075"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractEUCelex(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractEUCelex(%q) = %v; want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestExtractEUCelexDedupeSort(t *testing.T) {
	// Repeats and out-of-order forms must collapse to a sorted, deduped list.
	text := "(direktiv 2010/75/EU) og (direktiv 2010/75/EU) og (direktiv 2004/35/EF) og (direktiv 2010/75/EU)"
	got := extractEUCelex(text)
	if !reflect.DeepEqual(got, []string{"32004L0035", "32010L0075"}) {
		t.Fatalf("got %v", got)
	}
}

func TestEuRefToCELEXPassthrough(t *testing.T) {
	if got := euRefToCELEX("32003L0004"); got != "32003L0004" {
		t.Fatalf("passthrough = %q", got)
	}
	if got := euRefToCELEX("32009r0469"); got != "32009R0469" {
		t.Fatalf("lowercase passthrough = %q", got)
	}
}

func TestDirectiveToCELEXUnchanged(t *testing.T) {
	// Existing directive mapping behaviour must be preserved.
	cases := map[string]string{
		"2003/4/EF":   "32003L0004",
		"2014/40/EU":  "32014L0040",
		"2009/103/EF": "32009L0103",
	}
	for in, want := range cases {
		if got := directiveToCELEX(in); got != want {
			t.Fatalf("directiveToCELEX(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestEuDocToCELEX(t *testing.T) {
	if got := euDocToCELEX('L', 2003, 4); got != "32003L0004" {
		t.Fatalf("L = %q", got)
	}
	if got := euDocToCELEX('R', 2009, 469); got != "32009R0469" {
		t.Fatalf("R = %q", got)
	}
	if got := euDocToCELEX('D', 2008, 768); got != "32008D0768" {
		t.Fatalf("D = %q", got)
	}
}

func TestNormYear(t *testing.T) {
	if normYear(93) != 1993 || normYear(87) != 1987 || normYear(2009) != 2009 || normYear(60) != 1960 {
		t.Fatalf("normYear wrong: %d %d %d %d", normYear(93), normYear(87), normYear(2009), normYear(60))
	}
}

func TestExtractEUCelexNoOvermatch(t *testing.T) {
	// Prose digits without a directive/regulation/decision keyword must not
	// produce a CELEX (conservative parser).
	cases := []string{
		"EØS-avtalen vedlegg XVIII.",
		"EØS-avtalen vedlegg XI",
		"EØS-avtalen vedlegg I kap. I",
		"Lovens § 23 tredje ledd, jf. kapittel 4.",
	}
	for _, text := range cases {
		if got := extractEUCelex(text); len(got) != 0 {
			t.Fatalf("extractEUCelex(%q) = %v; want empty", text, got)
		}
	}
}
