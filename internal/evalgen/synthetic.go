package evalgen

import (
	"fmt"
	"math"
	"sort"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evaldata"
)

// SyntheticConfig controls the synthetic Q&A/MCQ generator.
type SyntheticConfig struct {
	SeedDir           string  // seed root (objects/ + relationships/)
	Out               string  // output JSONL path
	Limit             int     // total items to emit; 0 = all resolvable paragraphs
	UnanswerableRatio float64 // fraction of items that are deliberately unanswerable
	RandomSeed        int64   // determinism seed
}

// SyntheticResult summarises a synthetic run.
type SyntheticResult struct {
	Items        []evaldata.Item `json:"-"`
	Answerable   int             `json:"answerable"`
	Unanswerable int             `json:"unanswerable"`
	ResolvedRefs int             `json:"resolved_refs"` // answerable items each carry one validated ref
	Skipped      int             `json:"skipped"`       // paragraphs dropped (empty body)
	TotalParas   int             `json:"total_paras"`   // resolvable paragraphs seen
}

// unanswerablePoints is the canonical refusal gold point for unanswerable
// items. The harness uses it to check that the model refuses instead of
// hallucinating.
const unanswerablePoints = "Refuse / state the graph does not contain the answer"

// fabricatedSection is a section number that no real act in the corpus carries.
const fabricatedSection = "99-99"

// fabricatedActKey is an act key that does not exist in the corpus.
const fabricatedActKey = "lov/9999-99-99"

// GenerateSynthetic builds synthetic Q&A items plus a configurable fraction of
// unanswerable items, and writes them to cfg.Out as JSONL.
func GenerateSynthetic(cfg SyntheticConfig) (*SyntheticResult, error) {
	c, err := LoadCorpus(cfg.SeedDir)
	if err != nil {
		return nil, err
	}
	res := &SyntheticResult{TotalParas: len(c.paras)}

	rng := rngFor(cfg.RandomSeed)

	answerable, unanswerable := splitAnswerable(cfg.Limit, cfg.UnanswerableRatio)

	// Sample answerable paragraphs deterministically.
	idx := sampleIndices(rng, len(c.paras), answerable)
	selected := make([]paraMeta, 0, len(idx))
	for _, i := range idx {
		selected = append(selected, c.paras[i])
	}
	selected = sortParasByKey(selected)

	// Load content only for the sampled paragraphs.
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

		it := evaldata.NewItem()
		it.ID = fmt.Sprintf("synthetic-%05d", res.Answerable+1)
		it.Question = syntheticQuestion(actLabel(m, p.LawRefID), p.SectionLabel)
		it.GoldPoints = []string{body}
		it.GoldRefs = []string{ref}
		it.LegalArea = c.Area(p.LawRefID)
		it.Language = "nb"
		it.Source = "synthetic"
		it.Difficulty = "medium"
		it.NeedsCuration = false

		res.Items = append(res.Items, it)
		res.Answerable++
		res.ResolvedRefs++
	}

	// Build unanswerable items, alternating fabricated-section and fabricated-act.
	actKeys := c.ActKeys()
	for n := 0; n < unanswerable; n++ {
		it := evaldata.NewItem()
		it.ID = fmt.Sprintf("synthetic-unanswerable-%05d", n+1)
		it.GoldPoints = []string{unanswerablePoints}
		it.GoldRefs = []string{}
		it.Language = "nb"
		it.Source = "synthetic-unanswerable"
		it.Difficulty = "medium"
		it.Answerable = false
		it.NeedsCuration = false

		if n%2 == 0 && len(actKeys) > 0 {
			// Fabricated section on a real act.
			real := actKeys[rng.Intn(len(actKeys))]
			m := c.ActMeta(real)
			it.Question = syntheticQuestion(actLabel(m, real), "§ "+fabricatedSection)
			it.LegalArea = c.Area(real)
		} else {
			// Non-existent act key.
			it.Question = syntheticQuestion(fabricatedActKey, "§ 1")
			it.LegalArea = ""
		}

		res.Items = append(res.Items, it)
		res.Unanswerable++
	}

	if err := evaldata.WriteItemsFile(cfg.Out, res.Items); err != nil {
		return nil, err
	}
	return res, nil
}

// splitAnswerable splits a total item budget into answerable and unanswerable
// counts based on ratio (0..1).
func splitAnswerable(limit int, ratio float64) (answerable, unanswerable int) {
	if limit <= 0 {
		// No limit: caller passes an explicit budget elsewhere; here we clamp
		// ratio semantics to a sane pair.
		return 0, 0
	}
	unanswerable = int(math.Round(ratio * float64(limit)))
	if unanswerable > limit {
		unanswerable = limit
	}
	if unanswerable < 0 {
		unanswerable = 0
	}
	return limit - unanswerable, unanswerable
}

// sortParasByKey returns a copy of paras sorted by Key (stable output order).
func sortParasByKey(paras []paraMeta) []paraMeta {
	out := make([]paraMeta, len(paras))
	copy(out, paras)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
