package evaldata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AbbrevMap maps a normalised act token (abbreviation, short-title name, or
// full name) to a corpus act key such as "lov/2002-06-21-34".
//
// Norwegian exam texts reference acts by abbreviation ("fkjl", "avtl", "aml",
// "tvl", "strl", "skl", "sktl") or by colloquial name ("forbrukerkjøpsloven").
// The corpus has no name->key index, so we build one by scanning Law.jsonl.
type AbbrevMap map[string]string

// lawProperties is the subset of a Law object we need. Decoded streaming so the
// (large) Law.jsonl is never fully resident in memory.
type lawProperties struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Props struct {
		ShortTitle string `json:"short_title"`
		Name       string `json:"name"`
	} `json:"properties"`
}

// Token preference ranks. Exact matches (whole short_title, whole name, or a
// parenthetical/bracket segment) outrank hyphen/separator fragments, so e.g.
// "(skatteloven)" beats "Jan Mayen-skatteloven" for the token "skatteloven".
const (
	rankFragment = 1
	rankExact    = 2
)

// tokenRank pairs a normalised token with its preference rank and owning key.
type tokenRank struct {
	token string
	rank  int
	key   string
}

// isAmendmentLaw reports whether a law's name (lowercased) identifies it as an
// amendment, repeal, or enactment act rather than a substantive law, so its
// tokens must not be registered (they would otherwise steal the base law's
// short name).
func isAmendmentLaw(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range []string{
		"lov om endring",
		"lov om endringar",
		"lov om oppheving",
		"lov om opphevelse",
		"lov om ikraftsetting",
		"midlertidig lov om endring",
		"lov om gjennomføring av endring",
	} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// BuildAbbrevMap streams seed/objects/Law.jsonl and builds token->key mappings
// from short_title and name. Amendment acts are skipped, and for each token the
// highest-rank mapping wins (exact over fragment); on equal rank the earlier
// law key wins. The result is deterministic regardless of file order.
func BuildAbbrevMap(seedDir string) (AbbrevMap, error) {
	best := map[string]tokenRank{}
	path := filepath.Join(seedDir, "objects", "Law.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20) // allow very long lines
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var o lawProperties
		if err := json.Unmarshal(line, &o); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if o.Type != "Law" || o.Key == "" {
			continue
		}
		if isAmendmentLaw(o.Props.Name) {
			continue
		}
		for _, tr := range lawNameTokens(o.Props.ShortTitle, o.Props.Name) {
			tr.key = o.Key
			if prev, ok := best[tr.token]; !ok || rankBetter(tr, prev) {
				best[tr.token] = tr
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	m := make(AbbrevMap, len(best))
	for tok, tr := range best {
		m[tok] = tr.key
	}
	return m, nil
}

// rankBetter reports whether candidate cur should replace prev for a token:
// higher rank wins; on equal rank the earlier key (lexicographically smaller)
// wins.
func rankBetter(cur, prev tokenRank) bool {
	if cur.rank != prev.rank {
		return cur.rank > prev.rank
	}
	return cur.key < prev.key
}

// lawNameTokens returns the (token, rank) pairs contributed by a law's
// short_title and name. The whole short_title, the whole name, and any
// parenthetical or bracketed segment of the name rank EXACT (2); separator
// (hyphen etc.) fragments rank FRAGMENT (1).
func lawNameTokens(shortTitle, name string) []tokenRank {
	var out []tokenRank
	seen := map[string]int{} // token -> best rank seen so far

	add := func(tok string, rank int) {
		norm := NormalizeActToken(tok)
		if norm == "" {
			return
		}
		if prev, ok := seen[norm]; ok && prev >= rank {
			return
		}
		seen[norm] = rank
		out = append(out, tokenRank{token: norm, rank: rank})
	}

	add(shortTitle, rankExact)
	add(name, rankExact)
	for _, seg := range bracketSegments(name) {
		add(seg, rankExact)
	}
	for _, frag := range splitFragments(shortTitle) {
		add(frag, rankFragment)
	}
	for _, frag := range splitFragments(name) {
		add(frag, rankFragment)
	}
	return out
}

// bracketSegments returns the substrings inside (...) or [...] in s, in order.
func bracketSegments(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		open := strings.IndexAny(s[i:], "([")
		if open < 0 {
			break
		}
		open += i
		closeCh := byte(')')
		if s[open] == '[' {
			closeCh = ']'
		}
		close := strings.IndexByte(s[open+1:], closeCh)
		if close < 0 {
			break
		}
		close += open + 1
		out = append(out, s[open+1:close])
		i = close + 1
	}
	return out
}

// splitFragments returns the separator-split parts of s, excluding the full
// field fallback (which is registered separately as an exact token).
func splitFragments(s string) []string {
	parts := splitTitle(s)
	if len(parts) <= 1 {
		return nil
	}
	return parts[1:]
}

// splitTitle splits a short_title/name into candidate tokens on common
// separators ("–", "—", "-", "(", ")", "[", "]", ","). It also keeps the full
// field so multi-word names remain mappable.
func splitTitle(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	f := strings.NewReplacer("–", "\x00", "—", "\x00", "(", "\x00", ")", "\x00", "[", "\x00", "]", "\x00", ",", "\x00", "-", "\x00")
	parts := strings.Split(f.Replace(s), "\x00")
	out := make([]string, 0, len(parts)+1)
	out = append(out, s) // full field as a fallback token
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NormalizeActToken lowercases and strips trailing punctuation (dots) so that
// "strl." and "strl" collapse to one token.
func NormalizeActToken(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	s = strings.Trim(s, " .")
	return s
}

// Lookup returns the act key for a token, or "" when unknown.
func (m AbbrevMap) Lookup(token string) string {
	return m[NormalizeActToken(token)]
}
