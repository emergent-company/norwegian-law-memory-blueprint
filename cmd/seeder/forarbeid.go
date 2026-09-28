package main

import (
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// prepWork is a canonical, document-level preparatory-work reference.
type prepWork struct {
	Slug      string
	Name      string
	PrepType  string
	Session   string
	DocNumber string
	Year      string
}

// normalizeForarbeidSlug reduces a href to its canonical forarbeid slug:
// strip a leading "/", strip any #anchor, require the "forarbeid/" prefix, and
// collapse deep links ("dok21-202021/kap10" -> "dok21-202021").
func normalizeForarbeidSlug(href string) (string, bool) {
	s := stripAnchor(strings.TrimPrefix(strings.TrimSpace(href), "/"))
	if !strings.HasPrefix(s, "forarbeid/") {
		return "", false
	}
	slug := s[len("forarbeid/"):]
	if idx := strings.Index(slug, "/"); idx >= 0 {
		slug = slug[:idx]
	}
	if slug == "" {
		return "", false
	}
	return slug, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sessionFromCode formats a 6-digit parliamentary-session code ("199798",
// "202526", "196465") as "YYYY-YYYY" ("1997-1998", "2025-2026", "1964-1965").
// The session is start-year + next-year; parsing the year numerically handles the
// century rollover ("199900" -> 1999-2000).
func sessionFromCode(code string) string {
	if len(code) == 6 && isDigits(code) {
		y1, err := strconv.Atoi(code[:4])
		if err != nil {
			return ""
		}
		return strconv.Itoa(y1) + "-" + strconv.Itoa(y1+1)
	}
	return ""
}

// sessionYear returns the first year of a "YYYY-YYYY" session.
func sessionYear(session string) string {
	if len(session) >= 4 {
		return session[:4]
	}
	return ""
}

// firstDigits returns the first all-digits token in tokens, or "".
func firstDigits(tokens []string) string {
	for _, t := range tokens {
		if isDigits(t) {
			return t
		}
	}
	return ""
}

// classifyForarbeid maps a canonical slug to its (prep_type, session,
// document_number, year). It is conservative: an unparseable slug yields
// prep_type "unknown" with no number/session, rather than an invented value.
func classifyForarbeid(slug string) (prepType, session, docNumber, year string) {
	parts := strings.Split(slug, "-")
	if len(parts) == 0 {
		return "unknown", "", "", ""
	}
	prefix := parts[0]
	last := parts[len(parts)-1]

	switch {
	case prefix == "otprp":
		// otprp-N-SESS
		if len(parts) >= 3 {
			session = sessionFromCode(last)
			docNumber = firstDigits(parts[1 : len(parts)-1])
			return "proposisjon", session, docNumber, sessionYear(session)
		}
	case prefix == "prop":
		// prop-N[-L|-S|-LS|...]-SESS
		if len(parts) >= 3 {
			session = sessionFromCode(last)
			docNumber = firstDigits(parts[1 : len(parts)-1])
			return "proposisjon", session, docNumber, sessionYear(session)
		}
	case prefix == "inns" || prefix == "innb" || prefix == "inno":
		// inns[-o|-s|-l]-N-SESS or inns-N[-l|-s]-SESS
		if len(parts) >= 3 {
			session = sessionFromCode(last)
			docNumber = firstDigits(parts[1 : len(parts)-1])
			return "innstilling", session, docNumber, sessionYear(session)
		}
	case prefix == "lovvedtak":
		// lovvedtak-N-SESS
		if len(parts) >= 3 {
			session = sessionFromCode(last)
			docNumber = firstDigits(parts[1 : len(parts)-1])
			return "lovvedtak", session, docNumber, sessionYear(session)
		}
	case prefix == "nou":
		// nou-YYYY-NN (report number, no session)
		if len(parts) >= 3 {
			year = parts[1]
			docNumber = parts[2]
			return "nou", "", docNumber, year
		}
	case prefix == "stprp":
		// stprp-N-SESS
		if len(parts) >= 3 {
			session = sessionFromCode(last)
			docNumber = firstDigits(parts[1 : len(parts)-1])
			return "proposisjon", session, docNumber, sessionYear(session)
		}
	case strings.HasPrefix(prefix, "meld"):
		// meld[-st]-... -> melding
		if len(parts) >= 3 {
			session = sessionFromCode(last)
			docNumber = firstDigits(parts[1 : len(parts)-1])
			return "melding", session, docNumber, sessionYear(session)
		}
		return "melding", "", "", ""
	case strings.HasPrefix(prefix, "dok"):
		// dokDD-SESS
		if len(parts) >= 2 {
			session = sessionFromCode(last)
			return "dok", session, "", sessionYear(session)
		}
		return "dok", "", "", ""
	case prefix == "representantforslag":
		// representantforslag-N-SESS
		if len(parts) >= 3 {
			session = sessionFromCode(last)
			docNumber = firstDigits(parts[1 : len(parts)-1])
			return "representantforslag", session, docNumber, sessionYear(session)
		}
	case strings.HasPrefix(prefix, "lovanm"):
		// lovanm-SESS-NNN
		if len(parts) >= 3 {
			session = sessionFromCode(parts[1])
			docNumber = parts[2]
			return "lovanm", session, docNumber, sessionYear(session)
		}
		return "lovanm", "", "", ""
	}
	return "unknown", "", "", ""
}

// eraFor buckets a year into pre_1999 / 1999_plus / unknown.
func eraFor(year string) string {
	if year == "" {
		return "unknown"
	}
	if year < "1999" {
		return "pre_1999"
	}
	return "1999_plus"
}

// collectForarbeid walks the parsed document and captures every
// forarbeid/<slug> anchor: the canonical slug and its display text (the object
// name). Slugs are deduped (first text wins) and sorted.
func collectForarbeid(root *html.Node) (refs []string, names map[string]string) {
	names = make(map[string]string)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := attr(n, "href")
			if slug, ok := normalizeForarbeidSlug(href); ok {
				if _, exists := names[slug]; !exists {
					names[slug] = strings.TrimSpace(nodeText(n))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	for slug := range names {
		refs = append(refs, slug)
	}
	sort.Strings(refs)
	return refs, names
}

// aggregatePreparatoryWorks merges the per-document forarbeid references into a
// sorted, deduped list of canonical preparatory works. When two documents
// reference the same slug, the first document's anchor text wins (matching the
// object dedupe rule).
func aggregatePreparatoryWorks(docs []LovDoc) []prepWork {
	names := make(map[string]string)
	for _, d := range docs {
		for slug, name := range d.ForarbeidNames {
			if _, exists := names[slug]; !exists {
				names[slug] = name
			}
		}
	}
	slugs := make([]string, 0, len(names))
	for slug := range names {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	works := make([]prepWork, 0, len(slugs))
	for _, slug := range slugs {
		pt, session, num, year := classifyForarbeid(slug)
		works = append(works, prepWork{
			Slug:      slug,
			Name:      names[slug],
			PrepType:  pt,
			Session:   session,
			DocNumber: num,
			Year:      year,
		})
	}
	return works
}

// prepSummary is the aggregate used for the manifest preparatory_works block.
type prepSummary struct {
	Total        int            `json:"total"`
	WithFullText int            `json:"with_full_text"`
	ByType       map[string]int `json:"by_type"`
	ByEra        map[string]int `json:"by_era"`
}

// summarizePreparatoryWorks builds the manifest summary from the aggregated works.
func summarizePreparatoryWorks(works []prepWork) prepSummary {
	s := prepSummary{
		Total:        len(works),
		WithFullText: 0, // phase A: metadata-only
		ByType:       make(map[string]int),
		ByEra:        map[string]int{"pre_1999": 0, "1999_plus": 0, "unknown": 0},
	}
	for _, w := range works {
		s.ByType[w.PrepType]++
		s.ByEra[eraFor(w.Year)]++
	}
	return s
}
