package evalgen

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// norCaseHOLDTestURL is the canonical test split of the Nor-CaseHOLD dataset
// (CC-BY-4.0). It contains Norwegian case law — Høyesterett decisions and
// Skatteetaten binding-forhåndsuttalelser (BFU) — which is NOT part of this
// agent's statute/regulation graph. Items produced from it are therefore marked
// out_of_graph:true and are excluded from the main score by the harness.
const norCaseHOLDTestURL = "https://huggingface.co/datasets/bendik-eeg-henriksen/nor-casehold/resolve/main/test.jsonl"

// norCaseHOLDConfig default URL is overridable so tests can point at a fixture.
type NorCaseHOLDConfig struct {
	Out   string // output JSONL path
	URL   string // dataset URL; defaults to norCaseHOLDTestURL when empty
	Limit int    // max rows to map; 0 = all
}

// NorCaseHOLDRow is one record of the Nor-CaseHOLD test split. Rows sourced
// from "hoyesterett" carry per-sentence hm_score/rouge scores (the CaseHOLD
// holding-selection signal); rows sourced from "skatteetaten_bfu" carry a flat
// sentence split with no scores, and their summary is the gold holding.
type NorCaseHOLDRow struct {
	CaseName   string                `json:"case_name"`
	Source     string                `json:"source"`
	Sammendrag string                `json:"sammendrag"`
	FullText   string                `json:"full_text"`
	DocID      string                `json:"doc_id"`
	Date       string                `json:"date"`
	URL        string                `json:"url"`
	Sentences  []NorCaseHOLDSentence `json:"sentences"`
}

// NorCaseHOLDSentence is one candidate holding sentence.
type NorCaseHOLDSentence struct {
	Text    string  `json:"text"`
	HMScore float64 `json:"hm_score"`
	Rouge1F float64 `json:"rouge1_f"`
	Rouge2F float64 `json:"rouge2_f"`
}

// NorCaseHOLDResult summarises a nordercase run.
type NorCaseHOLDResult struct {
	Items []ItemExt `json:"-"`
	Count int       `json:"count"`
}

// GenerateNorCaseHOLD downloads the Nor-CaseHOLD test split and maps each row
// to a retrieval-shaped item flagged out_of_graph. Because the corpus is case
// law and not in the statute/regulation graph, these items carry no gold_refs
// and are for reference/methodology only — the harness excludes them from the
// main score.
func GenerateNorCaseHOLD(cfg NorCaseHOLDConfig) (*NorCaseHOLDResult, error) {
	url := cfg.URL
	if url == "" {
		url = norCaseHOLDTestURL
	}
	rows, err := fetchNorCaseHOLD(url)
	if err != nil {
		return nil, err
	}
	if cfg.Limit > 0 && cfg.Limit < len(rows) {
		rows = rows[:cfg.Limit]
	}

	items := make([]ItemExt, 0, len(rows))
	for i, r := range rows {
		items = append(items, mapNorCaseHOLD(r, i+1))
	}

	if err := WriteItemsExtFile(cfg.Out, items); err != nil {
		return nil, err
	}
	return &NorCaseHOLDResult{Items: items, Count: len(items)}, nil
}

// fetchNorCaseHOLD streams the JSONL dataset from url, decoding line by line.
func fetchNorCaseHOLD(url string) ([]NorCaseHOLDRow, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch nor-casehold: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch nor-casehold: HTTP %d", resp.StatusCode)
	}

	var rows []NorCaseHOLDRow
	dec := json.NewDecoder(resp.Body)
	for {
		var r NorCaseHOLDRow
		if err := dec.Decode(&r); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode nor-casehold: %w", err)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// mapNorCaseHOLD maps one dataset row to a retrieval-shaped item. The question
// asks which holding is correct; the gold point is the scored holding sentence
// (Høyesterett) or the summary (BFU). No gold_refs are emitted because case
// law is out of the statute/regulation graph.
func mapNorCaseHOLD(row NorCaseHOLDRow, n int) ItemExt {
	it := NewItemExt()
	it.ID = fmt.Sprintf("nor-casehold-%05d", n)
	it.Task = "retrieval"
	it.Source = "nor-casehold"
	it.OutOfGraph = true
	it.Language = "nb"
	it.License = "cc-by-4.0"
	it.Difficulty = "unknown"
	it.NeedsCuration = true

	label := row.CaseName
	if strings.TrimSpace(label) == "" {
		label = firstSentence(row.Sammendrag)
	}
	it.Question = fmt.Sprintf("Hvilket holdepunkt (holding) er korrekt for: %s?", capText(label, 200))

	it.GoldPoints = []string{goldHolding(row)}
	it.GoldRefs = []string{}
	it.DocRef = row.DocID
	it.SourceURL = row.URL

	return it
}

// goldHolding returns the gold holding for a row: the sentence with the highest
// hm_score when scores are present, else the summary.
func goldHolding(row NorCaseHOLDRow) string {
	best := ""
	bestScore := -1.0
	hasScore := false
	for _, s := range row.Sentences {
		if s.HMScore > 0 {
			hasScore = true
		}
		if s.HMScore > bestScore {
			bestScore = s.HMScore
			best = s.Text
		}
	}
	if hasScore && best != "" {
		return capText(normalizeSpace(best), goldPointCap)
	}
	return capText(normalizeSpace(row.Sammendrag), goldPointCap)
}

// firstSentence returns the first sentence of s (up to the first period), or s
// trimmed when no period is present.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i+1]
	}
	return s
}

// normalizeSpace collapses runs of whitespace to single spaces.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
