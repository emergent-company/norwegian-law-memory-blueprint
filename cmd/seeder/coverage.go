package main

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// skipClasses is the set of element classes excluded from both the rendered
// body and the coverage source-text extraction (change annotations, footnotes,
// and the table-of-contents list). It is package-scoped so extractBody and the
// coverage computation share one definition.
var skipClasses = map[string]bool{
	"changesToParent": true,
	"footnotes":       true,
	"tocSubUl":        true,
}

// blockElements is the set of element names treated as block boundaries in the
// source-text extraction. A single space is appended after each such element so
// adjacent blocks (a § header ending, a body paragraph starting, list items,
// table cells' parent rows) never merge into phantom tokens. Inline elements
// (<a>, <span>, <sup>, <td>, <th>, <tr>…) are NOT in this set, so their text
// stays concatenated exactly as extractBody's extractPlainText emits it — table
// cells merge identically on the source and rendered sides.
var blockElements = map[string]bool{
	"article": true, "section": true, "div": true, "p": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"table": true, "ol": true, "ul": true, "li": true, "br": true, "footer": true,
}

// tokenize splits s into a bag of tokens: each maximal run of Unicode letters
// or digits, lowercased. Punctuation and whitespace are stripped, so "§ 1-1"
// yields ["1","1"], "Formål" yields ["formål"], and markdown decoration
// ("###", "—", "-") disappears. The definition is deterministic and
// explainable: coverage measures body-word recall, not punctuation fidelity.
func tokenize(s string) []string {
	lower := strings.ToLower(s)
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return toks
}

// sourceBodyText returns the text of the main.documentBody subtree, excluding
// any element whose class is in skipClasses. It is boundary-aware: a space is
// appended after each block-level element (see blockElements) so runs never
// merge across blocks, while inline elements and their text nodes concatenate
// without a separator — matching how extractBody emits markdown (extractPlainText
// concatenates within a paragraph; "\n\n" separates blocks).
func sourceBodyText(main *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && skipClasses[attr(n, "class")] {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blockElements[n.Data] {
			sb.WriteByte(' ')
		}
	}
	walk(main)
	return sb.String()
}

// docModelText returns the concatenation of everything captured for a document:
// the rendered body (Content) plus the title fields (Title, ShortTitle) that
// extractBody deliberately places in metadata rather than Content. Coverage
// asks "is this source text represented in what we captured?", so the title is
// part of the model even though it is not in Content.
func docModelText(d *LovDoc) string {
	var sb strings.Builder
	for _, part := range []string{d.Content, d.Title, d.ShortTitle} {
		if part == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(part)
	}
	return sb.String()
}

// tokenCoverage returns the fraction of source tokens present in the captured
// model multiset (rendered body + title fields); 1.0 when the source has no
// tokens. Model tokens are counted as a multiset, so each source token consumes
// at most one model occurrence.
func tokenCoverage(main *html.Node, modelText string) float64 {
	src := tokenize(sourceBodyText(main))
	if len(src) == 0 {
		return 1.0
	}
	model := tokenize(modelText)
	rm := make(map[string]int, len(model))
	for _, t := range model {
		rm[t]++
	}
	matched := 0
	for _, t := range src {
		if rm[t] > 0 {
			rm[t]--
			matched++
		}
	}
	return float64(matched) / float64(len(src))
}

// coverageSummary aggregates per-document coverage.
type coverageSummary struct {
	Docs         int
	Mean         float64
	Min          float64
	DocsBelow099 int
	DocsBelow095 int
}

// computeCoverage aggregates per-doc Coverage values. Deterministic: docs are
// consumed in slice order and the arithmetic is pure.
func computeCoverage(docs []LovDoc) coverageSummary {
	s := coverageSummary{Docs: len(docs)}
	if len(docs) == 0 {
		return s
	}
	var sum float64
	s.Min = 1.0
	for _, d := range docs {
		c := d.Coverage
		sum += c
		if c < s.Min {
			s.Min = c
		}
		if c < 0.99 {
			s.DocsBelow099++
		}
		if c < 0.95 {
			s.DocsBelow095++
		}
	}
	s.Mean = sum / float64(len(docs))
	return s
}

// worstCoverage returns up to n docs with the lowest coverage, ascending.
func worstCoverage(docs []LovDoc, n int) []LovDoc {
	cp := append([]LovDoc(nil), docs...)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].Coverage < cp[j].Coverage })
	if len(cp) > n {
		cp = cp[:n]
	}
	return cp
}
