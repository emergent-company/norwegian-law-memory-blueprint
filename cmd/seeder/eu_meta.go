package main

import (
	"fmt"
	"strconv"
	"strings"
)

// ─── EU directive meta derivation ─────────────────────────────────────────────
//
// Two pure helpers derive the deterministic, CELEX-sourced properties of an EU
// act: its community/union marker and its canonical EUR-Lex ELI URL. Both are
// free of network I/O so they can be unit-tested offline.

// celexYear extracts the 4-digit year from a sector-3 CELEX id ("32003L0004" →
// 2003). Returns 0 when the id is not a well-formed sector-3 CELEX.
func celexYear(celex string) int {
	s := strings.ToUpper(strings.TrimSpace(celex))
	if len(s) < 6 || s[0] != '3' {
		return 0
	}
	y, err := strconv.Atoi(s[1:5])
	if err != nil {
		return 0
	}
	return y
}

// euCommunityFromCELEX derives the EU community/union marker from a sector-3
// CELEX id's year, per the Maastricht (1993-11-01) and Lisbon (2009-12-01)
// treaty boundaries: year < 1993 → "EEG" (EEC); 1993 ≤ year < 2009 → "EG" (EC);
// year ≥ 2009 → "EU". Unparseable ids fall back to "EU" (Euratom acts are not
// derivable from the year alone and are not produced here).
func euCommunityFromCELEX(celex string) string {
	switch y := celexYear(celex); {
	case y == 0:
		return "EU" // unparseable
	case y < 1993:
		return "EEG"
	case y < 2009:
		return "EG"
	default:
		return "EU"
	}
}

// euURLEurLexFromCELEX builds the canonical EUR-Lex ELI URL from a sector-3
// CELEX id ("3YYYY<T>NNNN", T ∈ {L,R,D}):
//
//	https://eur-lex.europa.eu/eli/<dir|reg|dec>/<year>/<number>
//
// Returns "" when the id is not a well-formed sector-3 CELEX.
func euURLEurLexFromCELEX(celex string) string {
	s := strings.ToUpper(strings.TrimSpace(celex))
	if len(s) < 6 || s[0] != '3' {
		return ""
	}
	year := s[1:5]
	if _, err := strconv.Atoi(year); err != nil {
		return ""
	}
	var seg string
	switch s[5] {
	case 'L':
		seg = "dir"
	case 'R':
		seg = "reg"
	case 'D':
		seg = "dec"
	default:
		return ""
	}
	number := strings.TrimLeft(s[6:], "0")
	if number == "" {
		number = "0"
	}
	return fmt.Sprintf("https://eur-lex.europa.eu/eli/%s/%s/%s", seg, year, number)
}
