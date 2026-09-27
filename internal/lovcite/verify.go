package lovcite

import (
	"sort"
	"strings"
)

// QuoteResult reports the outcome of a quote verification.
type QuoteResult struct {
	Matched bool
	// The provision (and excerpt) where the quote matched, when Matched is true.
	Paragraph *Paragraph
	// Best (closest) provision when Matched is false and at least one target was
	// resolved; nil when the reference itself did not resolve.
	Closest *Paragraph
	Excerpt string // matched or closest excerpt (whitespace-normalized, truncated)
}

// excerptLimit bounds the size of the context returned for a match/mismatch.
const excerptLimit = 320

// VerifyQuote resolves ref to its provision(s) and asserts that quote is a
// substring of the provision text after whitespace normalization. Matching is
// case-sensitive. When the reference resolves but no provision contains the
// quote, the closest provision (by word overlap) is reported with an excerpt.
func (idx *Index) VerifyQuote(ref, quote string) QuoteResult {
	resolution := idx.ResolveRef(ref)
	if !resolution.ActFound || len(resolution.Targets) == 0 {
		return QuoteResult{}
	}

	normQuote := NormalizeWhitespace(quote)
	for _, p := range resolution.Targets {
		normContent := NormalizeWhitespace(p.Content)
		if strings.Contains(normContent, normQuote) {
			return QuoteResult{
				Matched:   true,
				Paragraph: p,
				Excerpt:   excerptAround(normContent, normQuote),
			}
		}
	}

	closest := closestParagraph(resolution.Targets, normQuote)
	out := QuoteResult{Matched: false, Closest: closest}
	if closest != nil {
		out.Excerpt = truncate(NormalizeWhitespace(closest.Content), excerptLimit)
	}
	return out
}

// excerptAround returns the normalized content with the matched quote, trimmed
// to a bounded window centered on the match.
func excerptAround(normContent, normQuote string) string {
	i := strings.Index(normContent, normQuote)
	if i < 0 {
		return truncate(normContent, excerptLimit)
	}
	start := i - 80
	if start < 0 {
		start = 0
	}
	end := i + len(normQuote) + 80
	if end > len(normContent) {
		end = len(normContent)
	}
	ex := normContent[start:end]
	if start > 0 {
		ex = "…" + ex
	}
	if end < len(normContent) {
		ex = ex + "…"
	}
	return ex
}

// closestParagraph returns the provision whose normalized content shares the
// most words with the normalized quote.
func closestParagraph(targets []*Paragraph, normQuote string) *Paragraph {
	if len(targets) == 0 {
		return nil
	}
	quoteWords := wordSet(normQuote)
	best := targets[0]
	bestScore := -1
	for _, p := range targets {
		score := 0
		for w := range wordSet(NormalizeWhitespace(p.Content)) {
			if quoteWords[w] {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = p
		}
	}
	return best
}

func wordSet(s string) map[string]bool {
	words := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		words[w] = true
	}
	return words
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SortedKeys returns a sorted copy of the index's act keys (used for stable
// reporting and tests).
func (idx *Index) SortedActKeys() []string {
	keys := make([]string, 0, len(idx.actKeys))
	for k := range idx.actKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
