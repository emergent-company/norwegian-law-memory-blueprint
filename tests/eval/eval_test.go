package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evaldata"
	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
)

// integrityDatasetSpec is the committed eval set: golden core + curated items.
const integrityDatasetSpec = "evals/golden/core.jsonl,evals/curated/*.jsonl"

// envFirst returns the first non-empty environment variable among names.
func envFirst(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// floatEnv parses a float env var, returning def when unset or malformed.
func floatEnv(name string, def float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// buildOffline builds the abbreviation map and lovcite index from the seed dir.
func buildOffline() (*lovcite.Index, evaldata.AbbrevMap, error) {
	abbrevs, err := evaldata.BuildAbbrevMap(seedDir())
	if err != nil {
		return nil, nil, fmt.Errorf("build abbreviation map: %w", err)
	}
	idx, err := lovcite.Build(seedDir(), lovcite.BuildOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("build lovcite index: %w", err)
	}
	return idx, abbrevs, nil
}

// TestDatasetIntegrity validates the committed eval set offline (no creds/network).
func TestDatasetIntegrity(t *testing.T) {
	idx, _, err := buildOffline()
	if err != nil {
		t.Fatalf("build offline index: %v", err)
	}

	// EVAL_DATASET overrides the committed set, e.g. to validate a freshly
	// built dataset (evals/dataset/uio.jsonl) offline.
	spec := os.Getenv("EVAL_DATASET")
	if spec == "" {
		spec = integrityDatasetSpec
	}
	paths, err := resolveDatasets(spec)
	if err != nil {
		t.Fatalf("resolve datasets: %v", err)
	}
	items, err := loadItems(paths)
	if err != nil {
		t.Fatalf("dataset not valid JSONL: %v", err)
	}

	var failures []string
	seenID := map[string]string{}
	for _, li := range items {
		it := li.Item
		if prev, ok := seenID[it.ID]; ok {
			failures = append(failures, fmt.Sprintf("duplicate id %q in %s and %s", it.ID, prev, li.File))
		} else {
			seenID[it.ID] = li.File
		}

		if it.Answerable {
			if strings.TrimSpace(it.Question) == "" {
				failures = append(failures, fmt.Sprintf("item %s: answerable=true but empty question", it.ID))
			}
			if len(it.GoldPoints) == 0 {
				failures = append(failures, fmt.Sprintf("item %s: answerable=true but no gold_points", it.ID))
			}
		} else if len(it.GoldRefs) != 0 {
			failures = append(failures, fmt.Sprintf("item %s: answerable=false but has gold_refs %v", it.ID, it.GoldRefs))
		}

		for _, ref := range it.GoldRefs {
			res := idx.ResolveRef(ref)
			switch {
			case !res.ActFound:
				failures = append(failures, fmt.Sprintf("item %s: gold_ref %q: unknown act", it.ID, ref))
			case len(res.Targets) == 0:
				failures = append(failures, fmt.Sprintf("item %s: gold_ref %q: section not found", it.ID, ref))
			}
		}
	}

	if len(failures) > 0 {
		t.Fatalf("dataset integrity: %d failure(s):\n  %s", len(failures), strings.Join(failures, "\n  "))
	}
	t.Logf("dataset integrity OK: %d items across %d files", len(items), len(paths))
}

// TestAgentEval runs the live agent against the selected dataset, scores each
// answer, and writes a report. It skips when credentials are absent.
func TestAgentEval(t *testing.T) {
	base := envFirst("TEST_SERVER_URL", "MEMORY_SERVER_URL")
	token := envFirst("TEST_API_TOKEN", "MEMORY_PROJECT_API_KEY")
	if base == "" || token == "" {
		t.Skip("set TEST_SERVER_URL and TEST_API_TOKEN (or MEMORY_SERVER_URL / MEMORY_PROJECT_API_KEY) to run live agent eval")
	}

	transport := os.Getenv("EVAL_TRANSPORT")
	if transport == "" {
		transport = "a2a"
	}
	var ask func(ctx context.Context, question string) (string, error)
	if transport == "trigger" {
		projectID := envFirst("TEST_PROJECT_ID", "EVAL_PROJECT_ID")
		agentID := os.Getenv("EVAL_AGENT_ID")
		if projectID == "" || agentID == "" {
			t.Skip("trigger transport requires TEST_PROJECT_ID (or EVAL_PROJECT_ID) and EVAL_AGENT_ID")
		}
		ask = func(ctx context.Context, question string) (string, error) {
			return sendAgentTrigger(ctx, base, token, projectID, agentID, question)
		}
	} else {
		ask = func(ctx context.Context, question string) (string, error) {
			return sendAgentMessage(ctx, base, token, question)
		}
	}

	idx, abbrevs, err := buildOffline()
	if err != nil {
		t.Fatalf("build offline index: %v", err)
	}

	paths, err := resolveDatasets(os.Getenv("EVAL_DATASET"))
	if err != nil {
		t.Fatalf("resolve EVAL_DATASET: %v", err)
	}
	items, err := loadItems(paths)
	if err != nil {
		t.Fatalf("load items: %v", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Item.ID < items[j].Item.ID })

	limit := 0
	if v := os.Getenv("EVAL_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("EVAL_LIMIT %q is not an integer", v)
		}
		if n > 0 {
			limit = n
		}
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	minF1 := floatEnv("EVAL_MIN_CITATION_F1", 0)
	minRecall := floatEnv("EVAL_MIN_POINT_RECALL", 0)

	judge, judgeOn := judgeFromEnv()
	t.Logf("live eval: %d items, judge=%v, transport=%s", len(items), judgeOn, transport)

	ctx := context.Background()
	results := make([]ItemResult, 0, len(items))
	for _, li := range items {
		it := li.Item
		ir := ItemResult{
			ID:         it.ID,
			File:       filepath.Base(li.File),
			LegalArea:  it.LegalArea,
			Difficulty: it.Difficulty,
			Answerable: it.Answerable,
			Question:   it.Question,
			GoldPoints: it.GoldPoints,
			GoldRefs:   it.GoldRefs,
		}

		answer, aerr := ask(ctx, it.Question)
		if aerr != nil {
			ir.Error = aerr.Error()
			results = append(results, ir)
			t.Logf("[%s] ERROR: %v", it.ID, aerr)
			continue
		}
		ir.Answer = answer
		ir.Citation = ScoreCitations(answer, it.GoldRefs, idx, abbrevs)

		if it.Answerable {
			if judgeOn {
				jr, jerr := runJudge(ctx, judge, it.Question, answer, it.GoldPoints)
				if jerr != nil {
					t.Logf("[%s] judge error: %v", it.ID, jerr)
				} else {
					pr := jr.PointRecall
					f := jr.Faithful
					ir.PointRecall = &pr
					ir.Faithful = &f
					ir.JudgeReason = jr.Reason
				}
			}
		} else {
			rc := assessRefusal(answer, ir.Citation)
			ir.RefusalCorrect = &rc
		}

		results = append(results, ir)
		t.Logf("[%s] F1=%.2f recall=%.2f prec=%.2f res=%.2f cited=%v hallucinated=%v",
			it.ID, ir.Citation.F1, ir.Citation.Recall, ir.Citation.Precision,
			ir.Citation.ResolutionRate, ir.Citation.CitedRefs, ir.Citation.HallucinatedRefs)
	}

	rep := buildReport(paths, results)
	reportPath, werr := writeReport(blueprintDir(), rep)
	if werr != nil {
		t.Errorf("write report: %v", werr)
	} else {
		t.Logf("report written to %s", reportPath)
	}
	printSummary(rep)

	overall := rep.Aggregates["overall"]
	if overall.CitationF1 < minF1 {
		t.Errorf("citation F1 %.3f below EVAL_MIN_CITATION_F1 %.3f", overall.CitationF1, minF1)
	}
	if overall.PointRecall != nil && *overall.PointRecall < minRecall {
		t.Errorf("point recall %.3f below EVAL_MIN_POINT_RECALL %.3f", *overall.PointRecall, minRecall)
	}
}

// assessRefusal determines whether an answerable:false item was handled safely:
// the answer must state that it cannot find / does not contain the answer and
// must not cite an invented provision (hallucinated ref). An answer that asserts
// a rule (no refusal language) fails.
func assessRefusal(answer string, c CitationMetrics) bool {
	if len(c.HallucinatedRefs) > 0 {
		return false // cites an invented provision
	}
	return isRefusalText(answer)
}

// isRefusalText reports whether the answer signals that the answer is not
// available / cannot be found / is not contained in the knowledge source.
func isRefusalText(text string) bool {
	low := strings.ToLower(text)
	markers := []string{
		"kan ikke", "ikke kan", "kan dessverre ikke", "finner ikke", "ikke finne",
		"har ikke", "ikke har", "inneholder ikke", "ikke inneholder",
		"kan ikke besvare", "ikke besvare", "kan ikke svare", "ikke svare",
		"ikke tilgjengelig", "ikke mulig", "ikke dekket", "utenfor",
		"cannot find", "does not contain", "not contain", "not available",
		"no answer", "ikke finner", "ikke har grunnlag",
	}
	for _, m := range markers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}
