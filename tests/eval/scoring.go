// Package eval implements the evaluation harness for the norwegian-law-assistant
// agent: an offline dataset-integrity test and a live (credential-gated) agent
// eval with deterministic citation scoring and an optional LLM judge.
package eval

import (
	"sort"
	"strings"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evaldata"
	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
)

// CitationMetrics is the deterministic citation-scoring result for one answer.
type CitationMetrics struct {
	TotalCited       int      `json:"total_cited"`
	Resolved         int      `json:"resolved"`
	Hallucinated     int      `json:"hallucinated"`
	ResolutionRate   float64  `json:"resolution_rate"`
	Precision        float64  `json:"precision"`
	Recall           float64  `json:"recall"`
	F1               float64  `json:"f1"`
	GoldRefs         []string `json:"gold_refs"`
	CitedRefs        []string `json:"cited_refs"`
	HallucinatedRefs []string `json:"hallucinated_refs"`
}

// ScoreCitations extracts references from an agent answer, resolves them against
// the seed index, and computes precision/recall/F1 against the gold references.
// Partial credit (same act, different/missing section) counts 0.5 toward both
// precision and recall. Cited references that fail to resolve are reported as
// hallucinated.
func ScoreCitations(answer string, goldRefs []string, idx *lovcite.Index, abbrevs evaldata.AbbrevMap) CitationMetrics {
	m := CitationMetrics{GoldRefs: canonicalGold(goldRefs)}

	cands := evaldata.ExtractRefs(answer, abbrevs)
	resolved, unresolved := evaldata.ResolveCandidates(cands, idx)

	resolvedSet := map[string]struct{}{}
	for _, r := range resolved {
		resolvedSet[r] = struct{}{}
	}

	citedSet := map[string]struct{}{}
	hallucinatedSet := map[string]struct{}{}
	for _, r := range resolved {
		citedSet[r] = struct{}{}
	}
	for _, u := range unresolved {
		if u.Act == "" {
			continue // unattributed refs cannot be counted as hallucinated acts
		}
		c := canonicalRef(u.Act, u.Section)
		citedSet[c] = struct{}{}
		hallucinatedSet[c] = struct{}{}
	}

	m.Resolved = len(resolvedSet)
	m.Hallucinated = len(hallucinatedSet)
	m.TotalCited = len(citedSet)
	m.CitedRefs = sortedKeys(citedSet)
	m.HallucinatedRefs = sortedKeys(hallucinatedSet)

	if m.TotalCited > 0 {
		m.ResolutionRate = float64(m.Resolved) / float64(m.TotalCited)
	} else {
		m.ResolutionRate = 1.0
	}

	goldSet := map[string]struct{}{}
	for _, g := range m.GoldRefs {
		goldSet[g] = struct{}{}
	}

	m.Precision, m.Recall = prf(citedSet, goldSet)
	m.F1 = f1(m.Precision, m.Recall)
	return m
}

// canonicalGold normalises gold_refs ("lov/...#§N") to canonical "act#§section"
// form, deduplicated and sorted.
func canonicalGold(refs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		act, section := splitGoldRef(r)
		c := canonicalRef(act, section)
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// splitGoldRef splits a gold reference into act key + normalised section.
func splitGoldRef(ref string) (act, section string) {
	act, rest, _ := strings.Cut(ref, "#")
	section = lovcite.NormalizeParagraphNum(rest)
	return act, section
}

// canonicalRef renders act + section in the canonical "act#§section" form used
// for storage and comparison. A whole-act reference (empty section) stays bare.
func canonicalRef(act, section string) string {
	if section == "" {
		return act
	}
	return act + "#§" + section
}

// refParts decomposes a canonical reference into act key + normalised section.
func refParts(canon string) (act, section string) {
	act, rest, _ := strings.Cut(canon, "#")
	section = lovcite.NormalizeParagraphNum(rest)
	return act, section
}

// matchScore scores one reference against another: 1.0 exact (same act and
// section), 0.5 partial (same act, different/missing section), 0 otherwise.
func matchScore(a, b string) float64 {
	aa, sa := refParts(a)
	ab, sb := refParts(b)
	if aa != ab {
		return 0
	}
	if sa == sb {
		return 1.0
	}
	return 0.5
}

// bestMatch returns the best match score of ref against any member of set.
func bestMatch(ref string, set map[string]struct{}) float64 {
	best := 0.0
	for other := range set {
		if s := matchScore(ref, other); s > best {
			best = s
		}
	}
	return best
}

// prf computes precision and recall with partial credit (0.5). Recall is the
// mean best-match score of each gold ref against the cited set; precision is the
// mean best-match score of each cited ref against the gold set.
func prf(cited, gold map[string]struct{}) (precision, recall float64) {
	if len(gold) == 0 {
		recall = 1.0
	} else {
		sum := 0.0
		for g := range gold {
			sum += bestMatch(g, cited)
		}
		recall = sum / float64(len(gold))
	}

	if len(cited) == 0 {
		precision = 0.0
	} else {
		sum := 0.0
		for c := range cited {
			sum += bestMatch(c, gold)
		}
		precision = sum / float64(len(cited))
	}
	return precision, recall
}

// f1 returns the harmonic mean of precision and recall, 0 when undefined.
func f1(p, r float64) float64 {
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// sortedKeys returns the sorted keys of a set.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
