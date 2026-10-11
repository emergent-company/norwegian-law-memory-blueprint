package evalgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
)

// writeFixture builds a tiny seed directory (Law + LegalParagraph +
// IN_LEGAL_AREA) for offline end-to-end tests. It returns the seed dir path.
func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mustWrite := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("objects/Law.jsonl", `{"type":"Law","key":"lov/2020-01-01-1","properties":{"short_title":"Testloven – tl","name":"Lov om testing","content":"x"}}
{"type":"Law","key":"lov/2019-02-02-2","properties":{"short_title":"Andreloven","name":"Lov om andre ting","content":"x"}}
`)

	mustWrite("objects/LegalParagraph.001.jsonl", `{"type":"LegalParagraph","key":"lov/2020-01-01-1#paragraf-1","properties":{"law_ref_id":"lov/2020-01-01-1","paragraph_num":"§ 1","section_id":"paragraf-1","section_label":"§ 1","title":"Formålet","content":"### § 1 — Formålet\n\nDette er formålet med loven.","position":1}}
{"type":"LegalParagraph","key":"lov/2020-01-01-1#paragraf-2","properties":{"law_ref_id":"lov/2020-01-01-1","paragraph_num":"§ 2","section_id":"paragraf-2","section_label":"§ 2","title":"Virkeområde","content":"### § 2 — Virkeområde\n\nLoven gjelder i Norge.","position":2}}
{"type":"LegalParagraph","key":"lov/2019-02-02-2#ledd-1","properties":{"law_ref_id":"lov/2019-02-02-2","paragraph_num":"","section_id":"ledd-1","section_label":"ledd 1","content":"### ledd 1\n\nEn ledd-tekst.","position":1}}
`)

	mustWrite("relationships/IN_LEGAL_AREA.jsonl", `{"type":"IN_LEGAL_AREA","srcKey":"lov/2020-01-01-1","dstKey":"area_Arbeidsrett"}
{"type":"IN_LEGAL_AREA","srcKey":"lov/2019-02-02-2","dstKey":"area_Strafferett"}
`)

	return dir
}

func TestLoadCorpusFixture(t *testing.T) {
	dir := writeFixture(t)
	c, err := LoadCorpus(dir)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if got := len(c.paras); got != 3 {
		t.Fatalf("paras = %d, want 3", got)
	}
	if c.Area("lov/2020-01-01-1") != "Arbeidsrett" {
		t.Fatalf("area = %q", c.Area("lov/2020-01-01-1"))
	}
	if m := c.ActMeta("lov/2020-01-01-1"); m.ShortTitle != "Testloven – tl" {
		t.Fatalf("short_title = %q", m.ShortTitle)
	}
}

func TestGenerateSyntheticFixture(t *testing.T) {
	dir := writeFixture(t)
	out := filepath.Join(dir, "out.jsonl")

	res, err := GenerateSynthetic(SyntheticConfig{
		SeedDir:           dir,
		Out:               out,
		Limit:             2,
		UnanswerableRatio: 0,
		RandomSeed:        42,
	})
	if err != nil {
		t.Fatalf("GenerateSynthetic: %v", err)
	}
	if res.Answerable != 2 || res.Unanswerable != 0 {
		t.Fatalf("got answerable=%d unanswerable=%d, want 2/0", res.Answerable, res.Unanswerable)
	}

	idx, err := lovcite.Build(dir, lovcite.BuildOptions{})
	if err != nil {
		t.Fatalf("lovcite.Build: %v", err)
	}
	for _, it := range res.Items {
		if it.Source != "synthetic" {
			t.Fatalf("source = %q", it.Source)
		}
		if it.Language != "nb" || it.Difficulty != "medium" {
			t.Fatalf("language/difficulty = %q/%q", it.Language, it.Difficulty)
		}
		if len(it.GoldRefs) != 1 {
			t.Fatalf("gold_refs = %v, want exactly 1", it.GoldRefs)
		}
		r := idx.ResolveRef(it.GoldRefs[0])
		if !r.ActFound || len(r.Targets) == 0 {
			t.Fatalf("gold_ref %q does not resolve", it.GoldRefs[0])
		}
	}
}

func TestGenerateSyntheticUnanswerableFixture(t *testing.T) {
	dir := writeFixture(t)
	out := filepath.Join(dir, "out.jsonl")

	res, err := GenerateSynthetic(SyntheticConfig{
		SeedDir:           dir,
		Out:               out,
		Limit:             4,
		UnanswerableRatio: 0.5,
		RandomSeed:        7,
	})
	if err != nil {
		t.Fatalf("GenerateSynthetic: %v", err)
	}
	if res.Answerable != 2 || res.Unanswerable != 2 {
		t.Fatalf("got answerable=%d unanswerable=%d, want 2/2", res.Answerable, res.Unanswerable)
	}
	for _, it := range res.Items {
		if it.Source == "synthetic-unanswerable" {
			if it.Answerable {
				t.Fatalf("unanswerable item should be answerable:false")
			}
			if len(it.GoldRefs) != 0 {
				t.Fatalf("unanswerable item should have no gold_refs, got %v", it.GoldRefs)
			}
			if len(it.GoldPoints) != 1 || it.GoldPoints[0] != unanswerablePoints {
				t.Fatalf("unanswerable gold point = %v", it.GoldPoints)
			}
		}
	}
}

func TestGenerateSyntheticDeterministic(t *testing.T) {
	dir := writeFixture(t)

	a, err := GenerateSynthetic(SyntheticConfig{SeedDir: dir, Out: filepath.Join(dir, "a.jsonl"), Limit: 3, UnanswerableRatio: 0.2, RandomSeed: 42})
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateSynthetic(SyntheticConfig{SeedDir: dir, Out: filepath.Join(dir, "b.jsonl"), Limit: 3, UnanswerableRatio: 0.2, RandomSeed: 42})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != len(b.Items) {
		t.Fatalf("item count differs: %d vs %d", len(a.Items), len(b.Items))
	}
	for i := range a.Items {
		if a.Items[i].Question != b.Items[i].Question || a.Items[i].ID != b.Items[i].ID {
			t.Fatalf("item %d differs: %q vs %q", i, a.Items[i].Question, b.Items[i].Question)
		}
	}
}

func TestGenerateRetrievalFixture(t *testing.T) {
	dir := writeFixture(t)
	out := filepath.Join(dir, "retr.jsonl")

	res, err := GenerateRetrieval(RetrievalConfig{SeedDir: dir, Out: out, Limit: 2, RandomSeed: 1})
	if err != nil {
		t.Fatalf("GenerateRetrieval: %v", err)
	}
	if res.Count != 2 {
		t.Fatalf("count = %d, want 2", res.Count)
	}
	idx, err := lovcite.Build(dir, lovcite.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range res.Items {
		if it.Task != "retrieval" {
			t.Fatalf("task = %q", it.Task)
		}
		if it.DocRef == "" {
			t.Fatalf("doc_ref empty for %s", it.ID)
		}
		if len(it.GoldRefs) != 1 {
			t.Fatalf("gold_refs = %v", it.GoldRefs)
		}
		if r := idx.ResolveRef(it.GoldRefs[0]); !r.ActFound || len(r.Targets) == 0 {
			t.Fatalf("gold_ref %q does not resolve", it.GoldRefs[0])
		}
	}
}
