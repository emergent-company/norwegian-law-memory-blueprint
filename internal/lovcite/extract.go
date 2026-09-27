package lovcite

import (
	"regexp"
	"strings"
)

// actRefRE matches an explicit act reference: "lov/YYYY-MM-DD[-N]" or
// "forskrift/YYYY-MM-DD[-N]". The trailing numeric suffix (1–4 digits) is
// optional because some historic acts have only a date, e.g. "lov/1687-04-15".
var actRefRE = regexp.MustCompile(`(lov|forskrift)/\d{4}-\d{2}-\d{2}(?:-\d{1,4})?`)

// sectionRefRE matches a single section reference: "§" followed by optional
// whitespace, a flat number ("§ 22") or a chapterised number ("§ 1-3"), and an
// optional directly-attached letter suffix ("§ 1-3a"). The range separator may
// be a hyphen, en dash or em dash. "§§" (plural ranges/lists) is not matched by
// this expression; callers skip a "§" that is the second character of "§§".
var sectionRefRE = regexp.MustCompile(`§\s*(\d+(?:\s*[-–—]\s*\d+)?[a-zæøå]*)`)

// ActRef is an explicit act reference extracted from document content.
type ActRef struct {
	Raw   string // raw matched text, e.g. "lov/2005-06-17-62"
	Kind  string // "lov" or "forskrift"
	Start int    // byte offset of the match within the source line
	End   int
}

// SectionRef is a single section reference extracted from document content.
type SectionRef struct {
	Raw   string // raw matched text, e.g. "§ 1-3a"
	Token string // normalized token, e.g. "1-3a" (ready for byNum lookup)
	Start int    // byte offset of the match within the source line
	End   int
}

// ExtractActRefs returns every explicit act reference in a line of content.
func ExtractActRefs(line string) []ActRef {
	locs := actRefRE.FindAllStringSubmatchIndex(line, -1)
	refs := make([]ActRef, 0, len(locs))
	for _, loc := range locs {
		refs = append(refs, ActRef{
			Raw:   line[loc[0]:loc[1]],
			Kind:  line[loc[2]:loc[3]],
			Start: loc[0],
			End:   loc[1],
		})
	}
	return refs
}

// ExtractSectionRefs returns every section reference in a line of content,
// skipping the second "§" of a plural "§§" marker.
func ExtractSectionRefs(line string) []SectionRef {
	locs := sectionRefRE.FindAllStringSubmatchIndex(line, -1)
	refs := make([]SectionRef, 0, len(locs))
	for _, loc := range locs {
		start := loc[0]
		if start > 0 && line[start-1] == '§' {
			continue // part of "§§"
		}
		refs = append(refs, SectionRef{
			Raw:   line[start:loc[1]],
			Token: normalizeSectionToken(line[loc[2]:loc[3]]),
			Start: start,
			End:   loc[1],
		})
	}
	return refs
}

// IsHeadingLine reports whether a line is a Markdown heading (a section title
// such as "### § 1-1 — Lovens formål"). Headings are structural, not citations,
// and are skipped during extraction.
func IsHeadingLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// crossActWindow is the maximum number of bytes between an explicit act
// reference and a following section reference for the section reference to be
// attributed to that act instead of the owning document. The window is
// intentionally short and conservative.
const crossActWindow = 80

// attributeSectionRefs returns the act key that each section reference should be
// resolved against.
//
// Default: the owning document (the document whose content is being scanned).
//
// Conservative cross-act rule: if an explicit act reference (lov/… or
// forskrift/…) appears on the same line before the section reference, with no
// other section reference in between and within crossActWindow bytes, the section
// reference is resolved against that other act instead. This handles cases such
// as "… jf. forskrift/2001-06-15-599 § 4-3". Human-readable act names
// ("tvisteloven", "Grundloven", "lov om hittegods") are NOT mapped to acts
// (the corpus has no name→key index), so those section references fall back to
// the owning document.
func attributeSectionRefs(line string, owningDoc string, actRefs []ActRef, secRefs []SectionRef) []string {
	if len(secRefs) == 0 {
		return nil
	}
	// Build a sorted view of section-ref starts for the "no intervening §" test.
	secStarts := make([]int, len(secRefs))
	for i, s := range secRefs {
		secStarts[i] = s.Start
	}

	result := make([]string, len(secRefs))
	for i, s := range secRefs {
		result[i] = owningDoc
		for _, a := range actRefs {
			if a.End > s.Start {
				continue // act ref after the section ref
			}
			if s.Start-a.End > crossActWindow {
				continue // too far away
			}
			if hasSectionRefBetween(secStarts, a.End, s.Start) {
				continue // another section reference intervenes
			}
			result[i] = a.Raw
			break
		}
	}
	return result
}

// hasSectionRefBetween reports whether any section reference starts strictly
// between lo and hi (exclusive).
func hasSectionRefBetween(secStarts []int, lo, hi int) bool {
	for _, p := range secStarts {
		if p > lo && p < hi {
			return true
		}
	}
	return false
}
