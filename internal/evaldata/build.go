package evaldata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
)

// BuildConfig controls the build (extract -> pair -> reference-validate) step.
type BuildConfig struct {
	CacheDir    string // cache root; source documents live under <CacheDir>/<source>/
	Source      string // source name, e.g. "uio"
	SeedDir     string // committed seed directory (objects/ + relationships/)
	Out         string // output JSONL path
	Limit       int    // max items to emit; 0 = unlimited
	OCRMaxPages int    // max pages to OCR per scanned paper (0 -> default)
	NoOCR       bool   // disable OCR fallback for image-only exam papers
	Logf        func(string, ...interface{})
}

// BuildStats summarises a build run.
type BuildStats struct {
	Documents      int `json:"documents"`
	ExamPapers     int `json:"exam_papers"`
	Veiledninger   int `json:"veiledninger"`
	Items          int `json:"items"`
	WithGoldPoints int `json:"with_gold_points"`
	NoVeiledning   int `json:"no_veiledning"`
	OCRUsed        int `json:"ocr_used"`
	ResolvedRefs   int `json:"resolved_refs"`
	UnresolvedRefs int `json:"unresolved_refs"`
}

// BuildResult is the full output of a build run.
type BuildResult struct {
	Items      []Item          `json:"items"`
	Unresolved []UnresolvedRef `json:"-"`
	Stats      BuildStats      `json:"stats"`
}

// BuildDataset pairs cached exam papers with sensorveiledninger, extracts text,
// splits gold points, extracts and validates references, and returns the items.
func BuildDataset(cfg BuildConfig) (*BuildResult, error) {
	srcDir := filepath.Join(cfg.CacheDir, cfg.Source)
	manifest, err := LoadManifest(filepath.Join(srcDir, "manifest.json"))
	if err != nil {
		return nil, err
	}

	abbrevs, err := BuildAbbrevMap(cfg.SeedDir)
	if err != nil {
		return nil, fmt.Errorf("build abbreviation map: %w", err)
	}
	idx, err := lovcite.Build(cfg.SeedDir, lovcite.BuildOptions{})
	if err != nil {
		return nil, fmt.Errorf("build lovcite index: %w", err)
	}

	// Group documents by course+semester -> kind -> language.
	type docSet map[string]map[string]ManifestEntry // kind -> lang -> entry
	groups := map[string]docSet{}
	for _, e := range manifest.Entries {
		gk := e.Course + "\x00" + e.Semester
		if groups[gk] == nil {
			groups[gk] = docSet{}
		}
		if groups[gk][e.Kind] == nil {
			groups[gk][e.Kind] = map[string]ManifestEntry{}
		}
		groups[gk][e.Kind][e.Language] = e
	}

	res := &BuildResult{}
	res.Stats.Documents = len(manifest.Entries)

	gkeys := make([]string, 0, len(groups))
	for k := range groups {
		gkeys = append(gkeys, k)
	}
	sort.Strings(gkeys)

	for _, gk := range gkeys {
		parts := strings.Split(gk, "\x00")
		course, semester := parts[0], parts[1]
		set := groups[gk]

		oppgave := pickOppgave(set["oppgave"])
		if oppgave == nil {
			continue // no exam paper -> nothing to evaluate
		}
		res.Stats.ExamPapers++

		veiledning := pickVeiledning(set["veiledning"], oppgave.Language)
		if veiledning != nil {
			res.Stats.Veiledninger++
		}

		if cfg.Limit > 0 && res.Stats.Items >= cfg.Limit {
			break
		}

		item, unresolved, ocrUsed, err := buildItem(cfg, srcDir, course, semester, *oppgave, veiledning, abbrevs, idx)
		if err != nil {
			return nil, err
		}
		res.Items = append(res.Items, item)
		res.Unresolved = append(res.Unresolved, unresolved...)
		res.Stats.Items++
		if ocrUsed {
			res.Stats.OCRUsed++
		}
		if len(item.GoldPoints) > 0 {
			res.Stats.WithGoldPoints++
		}
		if veiledning == nil {
			res.Stats.NoVeiledning++
		}
		res.Stats.ResolvedRefs += len(item.GoldRefs)
		res.Stats.UnresolvedRefs += len(unresolved)
	}

	return res, nil
}

// pickOppgave selects the preferred exam paper: bokmål (nb) first, then any.
func pickOppgave(langs map[string]ManifestEntry) *ManifestEntry {
	if langs == nil {
		return nil
	}
	if e, ok := langs["nb"]; ok {
		return &e
	}
	for _, e := range langs {
		return &e
	}
	return nil
}

// pickVeiledning selects the preferred sensorveiledning: same language as the
// paper first, then any.
func pickVeiledning(langs map[string]ManifestEntry, pref string) *ManifestEntry {
	if langs == nil {
		return nil
	}
	if e, ok := langs[pref]; ok {
		return &e
	}
	for _, e := range langs {
		return &e
	}
	return nil
}

// buildItem assembles one evaluation item from a paired (or unpaired) document
// set, returning the item, any unresolved references, and whether OCR was used
// for the question.
func buildItem(cfg BuildConfig, srcDir, course, semester string, oppgave ManifestEntry, veiledning *ManifestEntry, abbrevs AbbrevMap, idx *lovcite.Index) (Item, []UnresolvedRef, bool, error) {
	it := NewItem()
	it.ID = fmt.Sprintf("%s-%s-%s", cfg.Source, strings.ToLower(course), semester)
	it.Course = course
	it.Semester = semester
	it.Language = oppgave.Language
	it.LegalArea = LegalAreaForCourse(course)
	it.SourceURL = oppgave.SourceURL
	it.OppgaveURL = oppgave.URL

	// The exam paper may be a scanned image-only PDF: OCR it when pdftotext
	// yields too little text. Sensorveiledninger stay on the text path.
	res, err := ExtractText(filepath.Join(srcDir, oppgave.LocalPath), ExtractOptions{
		OCRMaxPages: cfg.OCRMaxPages,
		NoOCR:       cfg.NoOCR,
		Logf:        cfg.Logf,
	})
	// A PDF that fails extraction yields an empty question rather than
	// aborting the whole run.
	question, ocrUsed := res.Text, res.OCRUsed
	if err != nil {
		question, ocrUsed = res.Text, false
	}
	it.Question = strings.TrimSpace(question)

	if veiledning != nil {
		it.VeiledningURL = veiledning.URL
		veilText, err := PDFToText(filepath.Join(srcDir, veiledning.LocalPath))
		if err != nil {
			veilText = ""
		}
		it.GoldPoints = SplitPoints(veilText)
		cands := ExtractRefs(veilText, abbrevs)
		resolved, unresolved := ResolveCandidates(cands, idx)
		it.GoldRefs = resolved
		for i := range unresolved {
			unresolved[i].Item = it.ID
		}
		// needs_curation stays true: legal_area/difficulty are placeholders and
		// gold-point quality is unverified in this initial pass.
		it.Answerable = len(it.GoldPoints) > 0 || len(it.GoldRefs) > 0
		return it, unresolved, ocrUsed, nil
	}

	// No veiledning: still emit the item, flagged for curation.
	it.GoldPoints = []string{}
	it.GoldRefs = []string{}
	it.NeedsCuration = true
	it.Answerable = false
	return it, nil, ocrUsed, nil
}

// WriteUnresolved writes unresolved references as JSONL sidecar.
func WriteUnresolved(path string, unresolved []UnresolvedRef) error {
	if len(unresolved) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, u := range unresolved {
		b, err := json.Marshal(u)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}
