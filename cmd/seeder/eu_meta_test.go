package main

import "testing"

func TestEuCommunityFromCELEX(t *testing.T) {
	cases := []struct {
		celex string
		want  string
	}{
		{"31960R0011", "EEG"}, // pre-Maastricht
		{"31987L0054", "EEG"},
		{"31993L0013", "EG"}, // Maastricht boundary
		{"31995L0046", "EG"},
		{"32003L0004", "EG"},
		{"32008D0768", "EG"},
		{"32009L0103", "EU"}, // Lisbon boundary
		{"32016R0679", "EU"},
		{"32023R1804", "EU"},
		{"11997E175", "EU"}, // non-sector-3 → unparseable fallback
		{"", "EU"},
		{"garbage", "EU"},
	}
	for _, tc := range cases {
		if got := euCommunityFromCELEX(tc.celex); got != tc.want {
			t.Fatalf("euCommunityFromCELEX(%q) = %q; want %q", tc.celex, got, tc.want)
		}
	}
}

func TestEuURLEurLexFromCELEX(t *testing.T) {
	cases := []struct {
		celex string
		want  string
	}{
		{"32016R0679", "https://eur-lex.europa.eu/eli/reg/2016/679"},
		{"31960R0011", "https://eur-lex.europa.eu/eli/reg/1960/11"},
		{"31995L0046", "https://eur-lex.europa.eu/eli/dir/1995/46"},
		{"32003L0004", "https://eur-lex.europa.eu/eli/dir/2003/4"},
		{"31993L0013", "https://eur-lex.europa.eu/eli/dir/1993/13"},
		{"32009R0469", "https://eur-lex.europa.eu/eli/reg/2009/469"},
		{"32008D0768", "https://eur-lex.europa.eu/eli/dec/2008/768"},
		{"32014L0040", "https://eur-lex.europa.eu/eli/dir/2014/40"},
		{"32023r1804", "https://eur-lex.europa.eu/eli/reg/2023/1804"}, // lowercase in, normalized
		{"32003X0004", ""}, // unknown sector
		{"11997E175", ""},  // non-sector-3
		{"", ""},
		{"garbage", ""},
	}
	for _, tc := range cases {
		if got := euURLEurLexFromCELEX(tc.celex); got != tc.want {
			t.Fatalf("euURLEurLexFromCELEX(%q) = %q; want %q", tc.celex, got, tc.want)
		}
	}
}
