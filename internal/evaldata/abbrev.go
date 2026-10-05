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

// BuildAbbrevMap streams seed/objects/Law.jsonl and builds token->key mappings
// from short_title and name. A token maps to the *last* law that declares it.
func BuildAbbrevMap(seedDir string) (AbbrevMap, error) {
	m := AbbrevMap{}
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
		addNameTokens(m, o.Key, o.Props.ShortTitle)
		addNameTokens(m, o.Key, o.Props.Name)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// addNameTokens extracts mappable tokens from a short_title / name field and
// registers each against key. The abbreviation ("fkjl") and the leading name
// ("Forbrukerkjøpsloven") are both registered.
func addNameTokens(m AbbrevMap, key, field string) {
	for _, tok := range splitTitle(field) {
		norm := NormalizeActToken(tok)
		if norm != "" {
			m[norm] = key
		}
	}
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
