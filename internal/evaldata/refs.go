package evaldata

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
)

// RefCandidate is a raw reference extracted from sensorveiledning text: an act
// key plus a normalised section token (e.g. "16" or "1-3a").
type RefCandidate struct {
	ActKey  string `json:"act"`
	Section string `json:"section"`
	Raw     string `json:"raw"` // the §-token as it appeared, e.g. "§ 16"
}

// UnresolvedRef records a reference that could not be resolved against the
// seed. These are written to a sidecar and never silently emitted as gold_refs.
type UnresolvedRef struct {
	Item    string `json:"item,omitempty"` // owning item id, when known
	Act     string `json:"act"`            // act key or "" when un-attributed
	Section string `json:"section"`
	Raw     string `json:"raw"`
	Reason  string `json:"reason"`
}

// actMention is a detected act reference at a byte position in a line.
type actMention struct {
	pos int
	act string
}

// explicitActKeyRegex matches an explicit corpus act key written out in prose
// or a link path, e.g. "lov/1999-03-26-14" or "forskrift/2006-02-17-204".
// Boundary validity is checked manually (see tokenBoundaryOK), like the
// abbreviation tokens.
var explicitActKeyRegex = regexp.MustCompile(`(?i)(?:lov|forskrift)/\d{4}-\d{2}-\d{2}-\d+`)

// actTokenRegex builds a single case-insensitive regex matching any mappable
// act token. Keys are ordered longest-first so the longest token wins (e.g.
// "sktfvl" before "fvl"). No \b is used because many names contain non-ASCII
// letters; boundary validity is checked manually.
func actTokenRegex(m AbbrevMap) *regexp.Regexp {
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.ContainsAny(k, " ") || len([]rune(k)) < 2 {
			continue // skip multi-word and 1-rune tokens
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Slice(keys, func(i, j int) bool {
		if len([]rune(keys[i])) != len([]rune(keys[j])) {
			return len([]rune(keys[i])) > len([]rune(keys[j]))
		}
		return keys[i] < keys[j]
	})
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('|')
		}
		sb.WriteString(regexp.QuoteMeta(k))
	}
	return regexp.MustCompile(`(?i)(?:` + sb.String() + `)`)
}

// ExtractRefs extracts every act-attributed section reference from Norwegian
// exam text, expanding abbreviations to corpus act keys via m.
//
// Attribution is a line-wise pass. A § token is owned by the nearest act
// mention on the same line (or, with a single distinct act on the line, that
// act). Cross-line attribution is bounded: an act carries forward only from an
// immediately preceding non-empty "heading" line that named exactly one act and
// had no § reference. References that cannot be attributed to any act are
// dropped (they cannot be resolved).
func ExtractRefs(text string, m AbbrevMap) []RefCandidate {
	re := actTokenRegex(m)
	if re == nil {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")

	currentAct := ""
	var out []RefCandidate
	for _, line := range strings.Split(text, "\n") {
		// Detect act mentions with manual boundary verification.
		var mentions []actMention
		for _, loc := range re.FindAllStringIndex(line, -1) {
			if !tokenBoundaryOK(line, loc[0], loc[1]) {
				continue
			}
			token := strings.TrimSuffix(line[loc[0]:loc[1]], ".")
			if act, ok := m[NormalizeActToken(token)]; ok && act != "" {
				mentions = append(mentions, actMention{pos: loc[0], act: act})
			}
		}

		// Explicit corpus act keys (lov/…, forskrift/…) written in the text also
		// count as act mentions, so following § tokens attribute to them.
		for _, loc := range explicitActKeyRegex.FindAllStringIndex(line, -1) {
			if !tokenBoundaryOK(line, loc[0], loc[1]) {
				continue
			}
			key := strings.ToLower(line[loc[0]:loc[1]])
			if key == "" || mentionAt(mentions, loc[0]) {
				continue
			}
			mentions = append(mentions, actMention{pos: loc[0], act: key})
		}
		sort.SliceStable(mentions, func(i, j int) bool { return mentions[i].pos < mentions[j].pos })

		// Section references from lovcite.
		secRefs := lovcite.ExtractSectionRefs(line)

		// Distinct acts on this line (for the single-act heuristic).
		lineActs := distinctActs(mentions)

		for _, sr := range secRefs {
			act := nearestBefore(mentions, sr.Start)
			if act == "" && len(lineActs) == 1 {
				act = lineActs[0]
			}
			if act == "" {
				act = currentAct
			}
			if act == "" {
				continue
			}
			out = append(out, RefCandidate{
				ActKey:  act,
				Section: sr.Token,
				Raw:     sr.Raw,
			})
		}

		// Bounded carry-forward: only a heading line (exactly one act mention
		// and no § reference) carries its act to following lines. Empty lines
		// neither establish nor clear a heading.
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(mentions) == 1 && len(secRefs) == 0 {
			currentAct = mentions[0].act
		} else {
			currentAct = ""
		}
	}
	return out
}

// tokenBoundaryOK checks that a regex match does not abut a letter or digit on
// either side (allowing a single trailing dot). Runes are decoded properly so
// multi-byte Norwegian letters (ø/å/æ) count as word characters.
func tokenBoundaryOK(s string, start, end int) bool {
	if start > 0 {
		if r, _ := utf8.DecodeLastRuneInString(s[:start]); isWordRune(r) {
			return false
		}
	}
	e := end
	if e < len(s) && s[e] == '.' {
		e++
	}
	if e < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[e:]); isWordRune(r) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// nearestBefore returns the act of the nearest act mention at or before pos,
// or "" when none precedes pos.
func nearestBefore(mentions []actMention, pos int) string {
	var act string
	for _, m := range mentions {
		if m.pos > pos {
			break
		}
		act = m.act
	}
	return act
}

// mentionAt reports whether mentions already contains an actMention at pos, so
// an explicit act key never double-counts an abbreviation at the same position.
func mentionAt(mentions []actMention, pos int) bool {
	for _, m := range mentions {
		if m.pos == pos {
			return true
		}
	}
	return false
}

// distinctActs returns the unique act keys in mentions, in first-seen order.
func distinctActs(mentions []actMention) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mentions {
		if !seen[m.act] {
			seen[m.act] = true
			out = append(out, m.act)
		}
	}
	return out
}

// ResolveCandidates validates each candidate against the seed index. Resolved
// references are normalised to "act#§section" form and returned sorted and
// deduplicated. Unresolved ones are returned for sidecar logging.
func ResolveCandidates(cands []RefCandidate, idx *lovcite.Index) (resolved []string, unresolved []UnresolvedRef) {
	seen := map[string]bool{}
	for _, c := range cands {
		if c.ActKey == "" || c.Section == "" {
			unresolved = append(unresolved, UnresolvedRef{Act: c.ActKey, Section: c.Section, Raw: c.Raw, Reason: "empty act or section"})
			continue
		}
		ref := c.ActKey + "#§" + c.Section
		res := idx.ResolveRef(ref)
		switch {
		case !res.ActFound:
			unresolved = append(unresolved, UnresolvedRef{Act: c.ActKey, Section: c.Section, Raw: c.Raw, Reason: "unknown act"})
		case len(res.Targets) == 0:
			unresolved = append(unresolved, UnresolvedRef{Act: c.ActKey, Section: c.Section, Raw: c.Raw, Reason: "section not found"})
		default:
			if !seen[ref] {
				seen[ref] = true
				resolved = append(resolved, ref)
			}
		}
	}
	sort.Strings(resolved)
	return resolved, unresolved
}
