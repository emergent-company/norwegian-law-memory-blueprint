package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ItemResult is the per-item evaluation outcome recorded in the report.
type ItemResult struct {
	ID             string          `json:"id"`
	File           string          `json:"file"`
	LegalArea      string          `json:"legal_area"`
	Difficulty     string          `json:"difficulty"`
	Answerable     bool            `json:"answerable"`
	Question       string          `json:"question"`
	GoldPoints     []string        `json:"gold_points"`
	GoldRefs       []string        `json:"gold_refs"`
	Answer         string          `json:"answer"`
	Error          string          `json:"error,omitempty"`
	Citation       CitationMetrics `json:"citation"`
	PointRecall    *float64        `json:"point_recall"`
	Faithful       *bool           `json:"faithful"`
	JudgeReason    string          `json:"judge_reason,omitempty"`
	RefusalCorrect *bool           `json:"refusal_correct,omitempty"`
}

// GroupAgg is the aggregate metric set for one group (overall / legal_area / difficulty).
type GroupAgg struct {
	Count             int      `json:"count"`
	CitationF1        float64  `json:"citation_f1"`
	CitationRecall    float64  `json:"citation_recall"`
	CitationPrecision float64  `json:"citation_precision"`
	ResolutionRate    float64  `json:"resolution_rate"`
	PointRecall       *float64 `json:"point_recall"`
	RefusalCorrect    int      `json:"refusal_correct"`
	RefusalTotal      int      `json:"refusal_total"`

	f1Sum, recallSum, precisionSum, resSum, prSum float64
	prN                                           int
}

// Report is the machine-readable evaluation report.
type Report struct {
	GeneratedAt string              `json:"generated_at"`
	Dataset     []string            `json:"dataset"`
	ItemCount   int                 `json:"item_count"`
	Items       []ItemResult        `json:"items"`
	Aggregates  map[string]GroupAgg `json:"aggregates"`
}

// buildReport assembles the report and its aggregates.
func buildReport(datasets []string, items []ItemResult) Report {
	rep := Report{
		GeneratedAt: time.Now().UTC().Format("2006-01-02T15-04-05Z"),
		Dataset:     datasets,
		ItemCount:   len(items),
		Items:       items,
		Aggregates:  map[string]GroupAgg{},
	}
	rep.Aggregates["overall"] = aggregateItems(items)

	byArea := groupBy(items, func(i ItemResult) string { return i.LegalArea })
	for _, k := range sortedGroupKeys(byArea) {
		rep.Aggregates["legal_area="+k] = aggregateItems(byArea[k])
	}
	byDiff := groupBy(items, func(i ItemResult) string { return i.Difficulty })
	for _, k := range sortedGroupKeys(byDiff) {
		rep.Aggregates["difficulty="+k] = aggregateItems(byDiff[k])
	}
	return rep
}

// aggregateItems computes the aggregate metrics over a set of items. Citation
// metrics are averaged over answerable items (those with gold refs); point recall
// over judged items; refusal correctness over answerable:false items.
func aggregateItems(items []ItemResult) GroupAgg {
	var g GroupAgg
	for _, it := range items {
		if it.Answerable {
			g.Count++
			g.f1Sum += it.Citation.F1
			g.recallSum += it.Citation.Recall
			g.precisionSum += it.Citation.Precision
			g.resSum += it.Citation.ResolutionRate
			if it.PointRecall != nil {
				g.prSum += *it.PointRecall
				g.prN++
			}
			continue
		}
		g.RefusalTotal++
		if it.RefusalCorrect != nil && *it.RefusalCorrect {
			g.RefusalCorrect++
		}
	}
	if g.Count > 0 {
		g.CitationF1 = g.f1Sum / float64(g.Count)
		g.CitationRecall = g.recallSum / float64(g.Count)
		g.CitationPrecision = g.precisionSum / float64(g.Count)
		g.ResolutionRate = g.resSum / float64(g.Count)
	}
	if g.prN > 0 {
		pr := g.prSum / float64(g.prN)
		g.PointRecall = &pr
	}
	return g
}

// groupBy partitions items by keyFn.
func groupBy(items []ItemResult, keyFn func(ItemResult) string) map[string][]ItemResult {
	m := map[string][]ItemResult{}
	for _, it := range items {
		k := keyFn(it)
		if k == "" {
			k = "unknown"
		}
		m[k] = append(m[k], it)
	}
	return m
}

// sortedGroupKeys returns the sorted keys of a group map for deterministic output.
func sortedGroupKeys(m map[string][]ItemResult) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeReport serialises rep to evals/results/<generated_at>.json under root and
// returns the written path.
func writeReport(root string, rep Report) (string, error) {
	dir := filepath.Join(root, "evals", "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, rep.GeneratedAt+".json")
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	return path, os.WriteFile(path, b, 0o644)
}

// printSummary writes a concise human-readable table to stdout and the test log.
func printSummary(rep Report) {
	order := []string{"overall"}
	var areaKeys, diffKeys []string
	for k := range rep.Aggregates {
		switch {
		case strings.HasPrefix(k, "legal_area="):
			areaKeys = append(areaKeys, k)
		case strings.HasPrefix(k, "difficulty="):
			diffKeys = append(diffKeys, k)
		}
	}
	sort.Strings(areaKeys)
	sort.Strings(diffKeys)
	order = append(order, areaKeys...)
	order = append(order, diffKeys...)

	fmt.Println("=== norwegian-law-assistant eval summary ===")
	fmt.Printf("%-28s %5s %8s %8s %8s %9s %9s %8s\n",
		"group", "n", "f1", "recall", "prec", "res_rate", "pt_recall", "refusal")
	for _, k := range order {
		g, ok := rep.Aggregates[k]
		if !ok {
			continue
		}
		pr := "n/a"
		if g.PointRecall != nil {
			pr = fmt.Sprintf("%.3f", *g.PointRecall)
		}
		refusal := "n/a"
		if g.RefusalTotal > 0 {
			refusal = fmt.Sprintf("%d/%d", g.RefusalCorrect, g.RefusalTotal)
		}
		fmt.Printf("%-28s %5d %8.3f %8.3f %8.3f %9.3f %9s %8s\n",
			k, g.Count, g.CitationF1, g.CitationRecall, g.CitationPrecision,
			g.ResolutionRate, pr, refusal)
	}
	fmt.Println("===========================================")
}
