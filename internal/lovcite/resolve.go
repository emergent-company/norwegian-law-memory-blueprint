package lovcite

import (
	"sort"
	"strings"
)

// Resolution is the result of resolving a citation reference to provisions.
type Resolution struct {
	Base     string       // the act part of the reference, e.g. "lov/2005-06-17-62"
	Fragment string       // the "#…" part, empty when absent
	ActFound bool         // whether Base is a known Law/Regulation key
	Targets  []*Paragraph // resolved provisions (sorted by Key); empty when unresolved
}

// ResolveRef resolves a citation reference to one or more provisions.
//
// Accepted forms:
//
//	lov/2005-06-17-62                          whole act
//	forskrift/2006-02-17-204                   whole act
//	lov/2005-06-17-62#§1-1                     a section, by printed number
//	lov/2005-06-17-62#§ 1-1                    same (whitespace tolerated)
//	lov/2005-06-17-62#kapittel-1-paragraf-1    a section, by section id
//
// A bare act reference resolves to every provision of that act (sorted by key).
func (idx *Index) ResolveRef(ref string) Resolution {
	base, fragment := splitRef(ref)
	res := Resolution{Base: base, Fragment: fragment}

	if !idx.HasAct(base) {
		return res
	}
	res.ActFound = true

	if fragment == "" {
		// Whole act: every paragraph (including those with empty paragraph_num).
		m := idx.bySection[base]
		for _, p := range m {
			res.Targets = append(res.Targets, p)
		}
		sortTargets(res.Targets)
		return res
	}

	if strings.HasPrefix(strings.TrimSpace(fragment), "§") {
		// §-form: resolve by normalized paragraph number.
		token := NormalizeParagraphNum(fragment)
		if p, ok := idx.byNum[base][token]; ok {
			res.Targets = append(res.Targets, p)
		}
		return res
	}

	// Otherwise treat the fragment as a section id.
	if p, ok := idx.bySection[base][fragment]; ok {
		res.Targets = append(res.Targets, p)
	}
	return res
}

// ResolveSection resolves a section token (already normalized, e.g. "1-3a")
// within a given act, returning the matching paragraph or nil.
func (idx *Index) ResolveSection(actKey, token string) *Paragraph {
	if p, ok := idx.byNum[actKey][token]; ok {
		return p
	}
	return nil
}

// splitRef splits a reference on the first "#". "lov/x#§1-1" -> ("lov/x", "§1-1").
func splitRef(ref string) (base, fragment string) {
	if i := strings.Index(ref, "#"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

func sortTargets(ps []*Paragraph) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Key < ps[j].Key })
}
