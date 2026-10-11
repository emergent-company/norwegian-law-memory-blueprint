package evalgen

import (
	"fmt"
)

// RetrievalConfig controls the corpus-derived retrieval generator.
type RetrievalConfig struct {
	SeedDir    string
	Out        string
	Limit      int   // items to emit; 0 = all resolvable paragraphs
	RandomSeed int64 // determinism seed
}

// RetrievalResult summarises a retrieval run.
type RetrievalResult struct {
	Items        []ItemExt `json:"-"`
	Count        int       `json:"count"`
	ResolvedRefs int       `json:"resolved_refs"`
	Skipped      int       `json:"skipped"`
	TotalParas   int       `json:"total_paras"`
}

// GenerateRetrieval builds corpus-derived retrieval items. Each item asks which
// provision in an act regulates a topic; the ground truth is carried in both
// doc_ref (the full paragraph key) and gold_refs (the lovcite-validated
// citation form of the same paragraph).
func GenerateRetrieval(cfg RetrievalConfig) (*RetrievalResult, error) {
	c, err := LoadCorpus(cfg.SeedDir)
	if err != nil {
		return nil, err
	}
	res := &RetrievalResult{TotalParas: len(c.paras)}

	rng := rngFor(cfg.RandomSeed)

	limit := cfg.Limit
	if limit <= 0 {
		limit = len(c.paras)
	}
	idx := sampleIndices(rng, len(c.paras), limit)
	selected := make([]paraMeta, 0, len(idx))
	for _, i := range idx {
		selected = append(selected, c.paras[i])
	}
	selected = sortParasByKey(selected)

	want := make(map[string]bool, len(selected))
	for _, p := range selected {
		want[p.Key] = true
	}
	content, err := LoadParagraphContent(cfg.SeedDir, want)
	if err != nil {
		return nil, err
	}

	for _, p := range selected {
		body := provisionGoldPoint(content[p.Key])
		if body == "" {
			res.Skipped++
			continue
		}
		m := c.ActMeta(p.LawRefID)
		ref := paragraphRef(p.LawRefID, p.ParagraphNum, p.SectionID)

		it := NewItemExt()
		it.ID = fmt.Sprintf("retrieval-%05d", res.Count+1)
		it.Question = retrievalQuestion(actLabel(m, p.LawRefID), sectionTitle(p))
		it.GoldPoints = []string{body}
		it.GoldRefs = []string{ref}
		it.LegalArea = c.Area(p.LawRefID)
		it.Language = "nb"
		it.Source = "synthetic-retrieval"
		it.Difficulty = "medium"
		it.NeedsCuration = false
		it.Task = "retrieval"
		it.DocRef = p.Key

		res.Items = append(res.Items, it)
		res.Count++
		res.ResolvedRefs++
	}

	if err := WriteItemsExtFile(cfg.Out, res.Items); err != nil {
		return nil, err
	}
	return res, nil
}
