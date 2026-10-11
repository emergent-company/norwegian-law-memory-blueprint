package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// euRefPattern matches a directive/regulation/decision reference in the
// eeaReferences prose. It is deliberately conservative: it only fires on the
// explicit keyword + "number/year" shape, so prose digits are never over-matched.
//
// Forms covered (verified against the corpus):
//
//	"direktiv 2009/103/EF"          year/number + suffix
//	"direktiv (EU) 2016/797"        year/number
//	"direktiv 87/54"                2-digit year, no suffix
//	"forordning (EF) nr. 469/2009"  number/year (marked by "nr.")
//	"forordning (EU) 2019/621"      year/number (no "nr.")
//	"beslutning nr. 768/2008/EF"    number/year (marked by "nr.")
//
// Group 1 = keyword, group 2 = optional "nr." marker, group 3/4 = the two numbers.
var euRefPattern = regexp.MustCompile(`(?i)(direktiv|forordning|beslutning)\s*(?:\([A-ZØÆÅ]{1,4}\)\s*)?(nr\.?\s*)?(\d{1,4})/(\d{1,4})`)

// celexSector3RE matches an already-explicit sector-3 CELEX id (legislation),
// e.g. "32003L0004", "32009R0469". Used for pass-through in the fetch path.
var celexSector3RE = regexp.MustCompile(`(?i)^3\d{4}[A-Z]\d{1,4}$`)

// euDocToCELEX maps a typed EU document to its CELEX id. typ is 'L' (directive),
// 'R' (regulation) or 'D' (decision); year is the full year, num the number.
func euDocToCELEX(typ byte, year, num int) string {
	return fmt.Sprintf("3%04d%c%04d", year, typ, num)
}

// normYear expands a 2-digit year to a full year. The corpus 2-digit years are
// all pre-2000 (87→1987, 93→1993, 60→1960), so 19xx is the correct expansion.
func normYear(y int) int {
	if y < 100 {
		return 1900 + y
	}
	return y
}

// euRefToCELEX maps a single reference string — an already-explicit CELEX id, or
// a directive "YYYY/NNN[/XX]" form — to its CELEX id. The general typed mapper is
// euDocToCELEX (used by extractEUCelex); this is the route for the fetch path.
func euRefToCELEX(ref string) string {
	s := strings.ToUpper(strings.TrimSpace(ref))
	if celexSector3RE.MatchString(s) {
		return s
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return ""
	}
	year, errY := strconv.Atoi(parts[0])
	num, errN := strconv.Atoi(parts[1])
	if errY != nil || errN != nil {
		return ""
	}
	return euDocToCELEX('L', normYear(year), num)
}

// euCelexMatch is one parsed CELEX reference plus the byte span of its match in
// the source prose. Used to isolate the clause around each reference.
type euCelexMatch struct {
	CELEX string
	Start int
	End   int
}

// extractEUCelexWithPositions parses header eeaReferences prose into deduped
// CELEX ids (uppercase) together with each match's byte span, so callers can
// isolate the clause containing a specific reference. It shares the exact
// type/year/num mapping and sanity checks of extractEUCelex (which delegates to
// it), so behaviour is identical. The first occurrence of a duplicate CELEX wins.
func extractEUCelexWithPositions(text string) []euCelexMatch {
	seen := make(map[string]bool)
	var out []euCelexMatch
	for _, idx := range euRefPattern.FindAllStringSubmatchIndex(text, -1) {
		// idx[0],idx[1] full match; idx[2:4] keyword; idx[4:6] "nr." marker;
		// idx[6:8] first number; idx[8:10] second number.
		if len(idx) < 10 {
			continue
		}
		kw := strings.ToLower(text[idx[2]:idx[3]])
		hasNr := idx[4] >= 0
		a, errA := strconv.Atoi(text[idx[6]:idx[7]])
		b, errB := strconv.Atoi(text[idx[8]:idx[9]])
		if errA != nil || errB != nil {
			continue
		}
		var typ byte
		var year, num int
		switch kw {
		case "direktiv":
			typ, year, num = 'L', a, b
		case "forordning":
			typ = 'R'
			if hasNr {
				num, year = a, b
			} else {
				year, num = a, b
			}
		case "beslutning":
			typ = 'D'
			if hasNr {
				num, year = a, b
			} else {
				year, num = a, b
			}
		default:
			continue
		}
		year = normYear(year)
		if year < 1950 || year > 2100 || num < 1 || num > 9999 {
			continue
		}
		celex := euDocToCELEX(typ, year, num)
		if seen[celex] {
			continue
		}
		seen[celex] = true
		out = append(out, euCelexMatch{CELEX: celex, Start: idx[0], End: idx[1]})
	}
	return out
}

// extractEUCelex parses header eeaReferences prose into a deduped, sorted list
// of CELEX ids (uppercase). References that don't resolve to a plausible
// directive/regulation/decision (sanity-checked year/number ranges) are skipped,
// so the seed never gains a CELEX from a false-positive prose-digit match.
func extractEUCelex(text string) []string {
	matches := extractEUCelexWithPositions(text)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.CELEX)
	}
	sort.Strings(out)
	return out
}
