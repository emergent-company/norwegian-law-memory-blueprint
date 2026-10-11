package evalgen

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMapNorCaseHOLDHoyesterett(t *testing.T) {
	row := NorCaseHOLDRow{
		CaseName:   "En dom",
		Source:     "hoyesterett",
		Sammendrag: "Sammendrag tekst.",
		DocID:      "",
		URL:        "https://example.test/dom",
		Sentences: []NorCaseHOLDSentence{
			{Text: "feil setning", HMScore: 0.0},
			{Text: "riktig holdepunkt", HMScore: 0.9},
			{Text: "annen setning", HMScore: 0.1},
		},
	}
	it := mapNorCaseHOLD(row, 1)

	if it.Source != "nor-casehold" {
		t.Fatalf("source = %q", it.Source)
	}
	if !it.OutOfGraph {
		t.Fatalf("out_of_graph should be true")
	}
	if it.Task != "retrieval" {
		t.Fatalf("task = %q", it.Task)
	}
	if it.License != "cc-by-4.0" {
		t.Fatalf("license = %q", it.License)
	}
	if len(it.GoldRefs) != 0 {
		t.Fatalf("gold_refs should be empty (out of graph), got %v", it.GoldRefs)
	}
	if len(it.GoldPoints) != 1 || it.GoldPoints[0] != "riktig holdepunkt" {
		t.Fatalf("gold point = %v, want the highest-hm_score sentence", it.GoldPoints)
	}
	if !strings.HasPrefix(it.Question, "Hvilket holdepunkt") {
		t.Fatalf("question = %q", it.Question)
	}
}

func TestMapNorCaseHOLDBFU(t *testing.T) {
	row := NorCaseHOLDRow{
		CaseName:   "En BFU",
		Source:     "skatteetaten_bfu",
		Sammendrag: "Oppsummeringen av saken.",
		DocID:      "bfu-doc",
		URL:        "https://example.test/bfu",
		Sentences: []NorCaseHOLDSentence{
			{Text: "faktumsetning uten score"},
		},
	}
	it := mapNorCaseHOLD(row, 2)

	if it.DocRef != "bfu-doc" {
		t.Fatalf("doc_ref = %q", it.DocRef)
	}
	if len(it.GoldPoints) != 1 || it.GoldPoints[0] != "Oppsummeringen av saken." {
		t.Fatalf("BFU gold point should be the summary, got %v", it.GoldPoints)
	}
}

func TestMapNorCaseHOLDNoCaseName(t *testing.T) {
	row := NorCaseHOLDRow{
		Source:     "hoyesterett",
		Sammendrag: "Første setning. Andre setning.",
		Sentences:  []NorCaseHOLDSentence{{Text: "x", HMScore: 0.5}},
	}
	it := mapNorCaseHOLD(row, 3)
	if !strings.Contains(it.Question, "Første setning.") {
		t.Fatalf("question should fall back to first sentence of summary, got %q", it.Question)
	}
}

func TestGoldHolding(t *testing.T) {
	scored := NorCaseHOLDRow{Sentences: []NorCaseHOLDSentence{
		{Text: "a", HMScore: 0.2}, {Text: "b", HMScore: 0.8},
	}}
	if got := goldHolding(scored); got != "b" {
		t.Fatalf("goldHolding scored = %q", got)
	}

	unscored := NorCaseHOLDRow{
		Sammendrag: "Sum  tekst.",
		Sentences:  []NorCaseHOLDSentence{{Text: "no score"}},
	}
	if got := goldHolding(unscored); got != "Sum tekst." {
		t.Fatalf("goldHolding unscored = %q", got)
	}
}

// TestGenerateNorCaseHOLDIntegration exercises the real download. It skips
// (not fails) when there is no network, so the suite stays offline-safe.
func TestGenerateNorCaseHOLDIntegration(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodHead, norCaseHOLDTestURL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("no network: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("dataset unreachable: HTTP %d", resp.StatusCode)
	}

	res, err := GenerateNorCaseHOLD(NorCaseHOLDConfig{
		Out:   t.TempDir() + "/nor-casehold.jsonl",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("GenerateNorCaseHOLD: %v", err)
	}
	if res.Count != 2 {
		t.Fatalf("count = %d, want 2", res.Count)
	}
	for _, it := range res.Items {
		if !it.OutOfGraph {
			t.Fatalf("item %s not marked out_of_graph", it.ID)
		}
	}
}
