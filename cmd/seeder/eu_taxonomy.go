package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ─── EU reference taxonomy ────────────────────────────────────────────────────
//
// The Norwegian↔EU cross-reference model classifies each EU reference found in
// Norwegian legal prose (the EEA header or a law/regulation body) into a
// reference_type, and enriches the edge with the surrounding clause, the
// referenced EU articles, and the matched trigger keyword. All helpers are pure
// (no I/O) so they can be unit-tested offline.

// euRefMeta carries the per-reference extraction result, keyed by UPPERCASE
// CELEX on the LovDoc during parsing and consumed at relationship build time.
type euRefMeta struct {
	Context  string   // surrounding clause, capped at maxEUContextChars runes
	Keyword  string   // lowercased matched trigger keyword ("" when none)
	Articles []string // normalized article refs, deduped + ascending
}

// maxEUContextChars caps reference_context at 240 characters (runes).
const maxEUContextChars = 240

// euRefTypeTable is the ordered (first-match-wins) keyword → reference_type
// mapping, matched case-insensitively against Norwegian legal prose.
var euRefTypeTable = []struct {
	refType string
	terms   []string
}{
	{"implements", []string{"gjennomfør", "gjennomføring"}},
	{"supplements", []string{"suppler", "utfyll"}},
	{"applies", []string{"gjelder direkte", "får direkte anvendelse", "anvendes direkte"}},
	{"complies_with", []string{"i samsvar med", "oppfyller", "etterlever"}},
	{"derogates_from", []string{"fravik"}},
	{"amended_by", []string{"endret ved", "endring av"}},
	{"repealed_by", []string{"opphevet ved", "oppheving av"}},
	{"references", []string{"viser til", "henviser til", "refererer til"}},
}

// classifyEUReferenceType returns the reference_type and the lowercased matched
// trigger keyword for a clause of Norwegian legal prose. The first term that
// matches (case-insensitively) wins; when none matches it returns
// ("references", ""). Single-word terms are stems, so the full inflected word
// (e.g. "supplerer" from "suppler") is returned as the keyword.
func classifyEUReferenceType(clause string) (refType, keyword string) {
	lower := strings.ToLower(clause)
	for _, e := range euRefTypeTable {
		for _, term := range e.terms {
			idx := findTerm(lower, term)
			if idx < 0 {
				continue
			}
			if strings.Contains(term, " ") {
				return e.refType, term
			}
			return e.refType, wordAt(lower, idx, len(term))
		}
	}
	return "references", ""
}

// findTerm returns the byte offset of the first occurrence of term in lower
// that starts at a word boundary (preceded by a non-letter or the start of the
// string), or -1. This keeps "viser til" from matching inside "henviser til"
// while still letting stems ("suppler") match inside inflected words
// ("supplerer") that begin with the stem.
func findTerm(lower, term string) int {
	offset := 0
	for {
		idx := strings.Index(lower[offset:], term)
		if idx < 0 {
			return -1
		}
		abs := offset + idx
		if abs == 0 || !prevIsLetter(lower, abs) {
			return abs
		}
		offset = abs + 1
	}
}

func prevIsLetter(s string, abs int) bool {
	if abs <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:abs])
	return isLetterRune(r)
}

// referenceTypeFromKeyword maps a lowercased trigger keyword back to its
// reference_type, defaulting to "references" when the keyword is empty/unknown.
func referenceTypeFromKeyword(keyword string) string {
	refType, _ := classifyEUReferenceType(keyword)
	return refType
}

// implementsEEARefType returns the reference_type for an IMPLEMENTS_EEA
// (EEA-header) edge. A header reference is itself a transposition declaration,
// so the default is "implements" when no explicit keyword is present; when a
// keyword is present the mapping wins.
func implementsEEARefType(keyword string) string {
	if keyword == "" {
		return "implements"
	}
	return referenceTypeFromKeyword(keyword)
}

// isPrimaryRef reports whether an EU reference is a primary (implementing)
// relationship: reference_type ∈ {implements, supplements}.
func isPrimaryRef(refType string) bool {
	return refType == "implements" || refType == "supplements"
}

// wordAt returns the full word (contiguous letters) in s that contains the term
// match at byte offset start of the given byte length. Used to return the
// inflected form ("supplerer") from a stem match ("suppler").
func wordAt(s string, start, length int) string {
	rs := []rune(s)
	startR := utf8.RuneCountInString(s[:start])
	endR := startR + utf8.RuneCountInString(s[start:start+length])
	for endR < len(rs) && isLetterRune(rs[endR]) {
		endR++
	}
	for startR > 0 && isLetterRune(rs[startR-1]) {
		startR--
	}
	return string(rs[startR:endR])
}

func isLetterRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r == 'æ' || r == 'ø' || r == 'å' || r == 'Æ' || r == 'Ø' || r == 'Å':
		return true
	}
	return false
}

// euArticleKeywordRE locates the "artikkel"/"art." keyword.
var euArticleKeywordRE = regexp.MustCompile(`(?i)\b(?:artikkel|art\.)`)

// euArticleProseRE matches the prose article form "6 nr. 1 bokstav a" plus an
// optional second letter ("og c" / ", c").
var euArticleProseRE = regexp.MustCompile(`^(?i)(\d{1,4})\s+(?:nr\.?|nummer)\s+(\d{1,4})\s+bokstav(?:en)?\s+([a-zæøå])(?:\s*(?:,|og)\s*([a-zæøå]))?`)

// euArticleRangeRE matches a bare article range ("13-15", also en-dash).
var euArticleRangeRE = regexp.MustCompile(`^(\d{1,4})\s*[-–]\s*(\d{1,4})`)

// euArticleTokenRE matches a bare/dotted article number ("6", "6.1", "6.1.a").
var euArticleTokenRE = regexp.MustCompile(`^(\d{1,4})(?:\.\d+)*(?:\.[a-zæøå])?`)

// parseArticleRefs parses one or more article tokens at the start of s,
// returning normalized tokens (e.g. "6.1.a", "13-15"), bytes consumed, and ok.
func parseArticleRefs(s string) (tokens []string, n int, ok bool) {
	if m := euArticleProseRE.FindStringSubmatch(s); m != nil {
		tokens = append(tokens, m[1]+"."+m[2]+"."+m[3])
		if m[4] != "" {
			tokens = append(tokens, m[1]+"."+m[2]+"."+m[4])
		}
		return tokens, len(m[0]), true
	}
	if m := euArticleRangeRE.FindStringSubmatch(s); m != nil {
		return []string{m[1] + "-" + m[2]}, len(m[0]), true
	}
	if m := euArticleTokenRE.FindStringSubmatch(s); m != nil {
		return []string{m[0]}, len(m[0]), true
	}
	return nil, 0, false
}

// extractEUArticles finds article references in a clause of Norwegian legal
// prose and returns the normalized, deduped, ascending-sorted list. Handles
// "artikkel 6.1.a", "art. 6", "artikkel 6 nr. 1 bokstav a", ranges "13-15",
// and comma lists "6.1.a, 6.1.c". Returns nil when none are found.
func extractEUArticles(clause string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range euArticleKeywordRE.FindAllStringIndex(clause, -1) {
		rest := clause[m[1]:]
		for {
			rest = strings.TrimLeft(rest, " \t\r\n")
			if rest == "" {
				break
			}
			tokens, n, ok := parseArticleRefs(rest)
			if !ok {
				break
			}
			for _, t := range tokens {
				if !seen[t] {
					seen[t] = true
					out = append(out, t)
				}
			}
			rest = rest[n:]
			rest = strings.TrimLeft(rest, " \t")
			if !strings.HasPrefix(rest, ",") {
				break
			}
			rest = rest[1:]
		}
	}
	sortEUArticles(out)
	return out
}

// sortEUArticles orders article refs in stable ascending (natural) order so
// "6" < "6.1.a" < "6.1.c" < "13-15".
func sortEUArticles(refs []string) {
	sort.Slice(refs, func(i, j int) bool { return naturalLess(refs[i], refs[j]) })
}

func naturalLess(a, b string) bool {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ca, cb := a[ia], b[ib]
		if isDigit(ca) && isDigit(cb) {
			sa, sb := ia, ib
			for ia < len(a) && isDigit(a[ia]) {
				ia++
			}
			for ib < len(b) && isDigit(b[ib]) {
				ib++
			}
			na, _ := strconv.Atoi(a[sa:ia])
			nb, _ := strconv.Atoi(b[sb:ib])
			if na != nb {
				return na < nb
			}
			continue
		}
		if ca != cb {
			return ca < cb
		}
		ia++
		ib++
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// euReferenceContext returns the sentence/clause of text containing the
// reference range [refStart, refEnd), capped at max runes, plus the trigger
// keyword and article refs classified on that clause. When text is empty or the
// range is invalid it returns ("", "", nil) — it never fabricates a context.
func euReferenceContext(text string, refStart, refEnd, max int) (string, string, []string) {
	if text == "" || refStart < 0 || refEnd < refStart || refEnd > len(text) {
		return "", "", nil
	}
	s, e := clauseBounds(text, refStart, refEnd)
	ctx := strings.TrimSpace(text[s:e])
	if ctx == "" {
		return "", "", nil
	}
	if max > 0 && utf8.RuneCountInString(ctx) > max {
		ctx = truncateRunes(ctx, max)
	}
	_, kw := classifyEUReferenceType(ctx)
	arts := extractEUArticles(ctx)
	return ctx, kw, arts
}

// truncateRunes truncates s to at most n runes.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// clauseBounds returns the [start, end) byte range of the clause containing the
// reference range [refStart, refEnd). Boundaries are newlines, semicolons, and
// periods that end a sentence (followed by whitespace).
func clauseBounds(text string, refStart, refEnd int) (int, int) {
	start := prevClauseBoundary(text, refStart)
	for start < refStart {
		switch text[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			goto forward
		}
	}
forward:
	return start, nextClauseBoundary(text, refEnd)
}

func prevClauseBoundary(text string, pos int) int {
	i := pos
	for i > 0 {
		c := text[i-1]
		if c == '\n' || c == ';' {
			return i
		}
		if c == '.' && (i >= len(text) || text[i] == ' ' || text[i] == '\t' || text[i] == '\n') {
			return i
		}
		i--
	}
	return 0
}

func nextClauseBoundary(text string, pos int) int {
	for i := pos; i < len(text); i++ {
		c := text[i]
		if c == '\n' || c == ';' {
			return i
		}
		if c == '.' && (i+1 >= len(text) || text[i+1] == ' ' || text[i+1] == '\t' || text[i+1] == '\n') {
			return i
		}
	}
	return len(text)
}
