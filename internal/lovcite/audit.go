package lovcite

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultMaxUnresolved is the sentinel used when the caller does not pass
// --max-unresolved. It is the maximum int value, so the audit is report-only by
// default and never fails on unresolved references. Pass a concrete number to
// make the audit fail when more than that many references are unresolved.
const DefaultMaxUnresolved = int(^uint(0) >> 1)

// UnresolvedSample is one unresolved reference with its document context.
type UnresolvedSample struct {
	File   string `json:"file"`   // source file, e.g. "Law.jsonl"
	Key    string `json:"key"`    // owning document key, e.g. "lov/1893-07-20-2"
	Ref    string `json:"ref"`    // raw reference text, e.g. "§ 4" or "lov/2005-05-20-28"
	Kind   string `json:"kind"`   // "act" or "section"
	Reason string `json:"reason"` // short explanation
}

// AuditSummary is the deterministic result of an audit run.
type AuditSummary struct {
	ScannedDocs          int                `json:"scanned_docs"`
	ActLovFound          int                `json:"act_lov_found"`
	ActLovResolved       int                `json:"act_lov_resolved"`
	ActForskriftFound    int                `json:"act_forskrift_found"`
	ActForskriftResolved int                `json:"act_forskrift_resolved"`
	SectionFound         int                `json:"section_found"`
	SectionResolved      int                `json:"section_resolved"`
	TotalFound           int                `json:"total_found"`
	TotalResolved        int                `json:"total_resolved"`
	TotalUnresolved      int                `json:"total_unresolved"`
	ResolutionRate       float64            `json:"resolution_rate"`
	Samples              []UnresolvedSample `json:"samples"`
}

// Audit scans the Law/Regulation content of the committed seed and validates
// every extracted citation against the index. It returns the summary (not
// failing) so the caller can apply the --max-unresolved policy.
func (idx *Index) Audit(seedDir string, samples int) (AuditSummary, error) {
	var s AuditSummary
	var allUnresolved []UnresolvedSample

	objFiles, err := filepath.Glob(filepath.Join(seedDir, "objects", "*.jsonl"))
	if err != nil {
		return s, err
	}
	sort.Strings(objFiles)

	for _, path := range objFiles {
		fileName := filepath.Base(path)
		f, err := os.Open(path)
		if err != nil {
			return s, err
		}

		dec := json.NewDecoder(f)
		for {
			var o rawObject
			if err := dec.Decode(&o); err == io.EOF {
				break
			} else if err != nil {
				f.Close()
				return s, fmt.Errorf("%s: %w", path, err)
			}
			if o.Type != "Law" && o.Type != "Regulation" {
				continue
			}
			s.ScannedDocs++

			for _, line := range strings.Split(o.Properties.Content, "\n") {
				if IsHeadingLine(line) {
					continue
				}
				actRefs := ExtractActRefs(line)
				secRefs := ExtractSectionRefs(line)
				if len(actRefs) == 0 && len(secRefs) == 0 {
					continue
				}

				// Explicit act references.
				for _, a := range actRefs {
					if a.Kind == "lov" {
						s.ActLovFound++
					} else {
						s.ActForskriftFound++
					}
					if idx.HasAct(a.Raw) {
						if a.Kind == "lov" {
							s.ActLovResolved++
						} else {
							s.ActForskriftResolved++
						}
					} else {
						allUnresolved = append(allUnresolved, UnresolvedSample{
							File: fileName, Key: o.Key, Ref: a.Raw,
							Kind: "act", Reason: "unknown act",
						})
					}
				}

				// Section references (attributed to owning doc or a preceding act).
				attrib := attributeSectionRefs(line, o.Key, actRefs, secRefs)
				for i, sr := range secRefs {
					s.SectionFound++
					target := idx.ResolveSection(attrib[i], sr.Token)
					if target != nil {
						s.SectionResolved++
					} else {
						reason := fmt.Sprintf("section %q not found in %s", sr.Token, attrib[i])
						allUnresolved = append(allUnresolved, UnresolvedSample{
							File: fileName, Key: o.Key, Ref: sr.Raw,
							Kind: "section", Reason: reason,
						})
					}
				}
			}
		}
		f.Close()
	}

	s.TotalFound = s.ActLovFound + s.ActForskriftFound + s.SectionFound
	s.TotalResolved = s.ActLovResolved + s.ActForskriftResolved + s.SectionResolved
	s.TotalUnresolved = s.TotalFound - s.TotalResolved
	if s.TotalFound > 0 {
		s.ResolutionRate = float64(s.TotalResolved) / float64(s.TotalFound)
	}
	s.Samples = sampleUnresolved(allUnresolved, samples)
	return s, nil
}

// sampleUnresolved returns up to n samples, spread evenly across the input so
// the output is representative and deterministic.
func sampleUnresolved(all []UnresolvedSample, n int) []UnresolvedSample {
	if n <= 0 || len(all) == 0 {
		return nil
	}
	if len(all) <= n {
		out := append([]UnresolvedSample(nil), all...)
		return out
	}
	out := make([]UnresolvedSample, 0, n)
	step := float64(len(all)) / float64(n)
	for i := 0; i < n; i++ {
		out = append(out, all[int(float64(i)*step)])
	}
	return out
}
