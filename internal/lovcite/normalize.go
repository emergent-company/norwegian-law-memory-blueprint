package lovcite

import "strings"

// NormalizeParagraphNum strips a leading section marker ("§", "§§") and collapses
// runs of whitespace to single spaces, trimming the result.
//
//	"§ 1-3"   -> "1-3"
//	"§§ 6"    -> "6"
//	"§ 1-1 a" -> "1-1 a"   (the letter suffix keeps its space)
//	"§1-1a"   -> "1-1a"
func NormalizeParagraphNum(s string) string {
	return strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(s), "§")), " ")
}

// NormalizeWhitespace collapses every run of whitespace (spaces, tabs, newlines,
// non-breaking spaces) into a single space and trims the ends. It is used for
// quote comparison so that line breaks and indentation in the corpus do not
// affect a match.
func NormalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// normalizeSectionToken normalises the numeric part of an extracted section
// reference. It converts en-dash/em-dash range separators to hyphen and removes
// any spaces, so "1 - 3", "1–3" and "1-3" all become "1-3".
func normalizeSectionToken(t string) string {
	t = strings.ReplaceAll(t, "\u2013", "-") // en dash
	t = strings.ReplaceAll(t, "\u2014", "-") // em dash
	t = strings.ReplaceAll(t, " ", "")
	t = strings.ReplaceAll(t, "\t", "")
	return t
}
