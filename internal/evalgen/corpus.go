package evalgen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
)

// paraMeta is the subset of a LegalParagraph object we retain for generation.
// Content is deliberately excluded: it is loaded lazily for the sampled
// paragraphs only, keeping the 138k-paragraph corpus from being held in memory
// in full.
type paraMeta struct {
	Key          string // e.g. "lov/2005-06-17-62#kapittel-1-paragraf-1"
	LawRefID     string // parent Law/Regulation key, e.g. "lov/2005-06-17-62"
	Name         string // e.g. "§ 1-1 Lovens formål" or "15 Art"
	SectionLabel string // e.g. "§ 1-1", "15 Art", "ledd 1"
	SectionID    string // e.g. "kapittel-1-paragraf-1"
	ParagraphNum string // printed number, e.g. "§ 1-1" (may be empty)
	Title        string // e.g. "Lovens formål" (may be empty)
	Position     int
}

// actMeta is the subset of a Law/Regulation object we retain.
type actMeta struct {
	ShortTitle string
	Name       string
	Kind       string // "lov" or "forskrift"
}

// Corpus is the in-memory generation view of the seed: a lovcite index for ref
// validation, all resolvable paragraph metadata (sorted by key), act metadata,
// and an act→legal-area map.
type Corpus struct {
	idx   *lovcite.Index
	paras []paraMeta
	acts  map[string]actMeta
	areas map[string]string // act key -> primary area name (deterministic)
}

// Index returns the lovcite index used for reference validation.
func (c *Corpus) Index() *lovcite.Index { return c.idx }

// ActMeta returns the metadata for an act key (zero value when unknown).
func (c *Corpus) ActMeta(key string) actMeta { return c.acts[key] }

// Area returns the primary legal area for an act key, or "".
func (c *Corpus) Area(key string) string { return c.areas[key] }

// Paragraphs returns the sorted, resolvable paragraph metadata.
func (c *Corpus) Paragraphs() []paraMeta { return c.paras }

// ActKeys returns a sorted slice of all Law/Regulation keys.
func (c *Corpus) ActKeys() []string {
	keys := make([]string, 0, len(c.acts))
	for k := range c.acts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// rawObjectProps is the streaming-decode shape of a seed object.
type rawObjectProps struct {
	Type       string `json:"type"`
	Key        string `json:"key"`
	Properties struct {
		ShortTitle   string `json:"short_title"`
		Name         string `json:"name"`
		LawRefID     string `json:"law_ref_id"`
		ParagraphNum string `json:"paragraph_num"`
		SectionID    string `json:"section_id"`
		SectionLabel string `json:"section_label"`
		Title        string `json:"title"`
		Position     int    `json:"position"`
		Content      string `json:"content"`
	} `json:"properties"`
}

// rawRel is the streaming-decode shape of a seed relationship.
type rawRel struct {
	Type   string `json:"type"`
	SrcKey string `json:"srcKey"`
	DstKey string `json:"dstKey"`
}

// LoadCorpus streams the seed objects and relationships into a Corpus. Object
// files and relationship files are processed in sorted order for determinism.
func LoadCorpus(seedDir string) (*Corpus, error) {
	idx, err := lovcite.Build(seedDir, lovcite.BuildOptions{RetainContent: false})
	if err != nil {
		return nil, fmt.Errorf("build lovcite index: %w", err)
	}

	c := &Corpus{
		idx:   idx,
		acts:  map[string]actMeta{},
		areas: map[string]string{},
	}

	if err := c.loadActs(seedDir); err != nil {
		return nil, err
	}
	if err := c.loadAreas(seedDir); err != nil {
		return nil, err
	}
	if err := c.loadParagraphs(seedDir); err != nil {
		return nil, err
	}
	return c, nil
}

// loadActs streams Law.jsonl and Regulation.*.jsonl to build the act-metadata map.
func (c *Corpus) loadActs(seedDir string) error {
	for _, pattern := range []string{"objects/Law.jsonl", "objects/Regulation.*.jsonl"} {
		files, err := filepath.Glob(filepath.Join(seedDir, pattern))
		if err != nil {
			return err
		}
		sort.Strings(files)
		for _, f := range files {
			if err := scanObjects(f, func(o rawObjectProps) {
				if o.Type != "Law" && o.Type != "Regulation" {
					return
				}
				c.acts[o.Key] = actMeta{
					ShortTitle: o.Properties.ShortTitle,
					Name:       o.Properties.Name,
					Kind:       kindOfKey(o.Key),
				}
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadAreas streams IN_LEGAL_AREA.jsonl and records the primary (top-level)
// area for each act. Where an act has several top-level areas, the
// lexicographically first is chosen for determinism.
func (c *Corpus) loadAreas(seedDir string) error {
	path := filepath.Join(seedDir, "relationships", "IN_LEGAL_AREA.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Collect all top-level areas per act, then pick the first sorted one.
	areaSets := map[string]map[string]struct{}{}
	dec := json.NewDecoder(f)
	for {
		var r rawRel
		if err := dec.Decode(&r); err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if r.Type != "IN_LEGAL_AREA" || !strings.HasPrefix(r.DstKey, "area_") {
			continue
		}
		name := strings.TrimPrefix(r.DstKey, "area_")
		if name == "" {
			continue
		}
		if areaSets[r.SrcKey] == nil {
			areaSets[r.SrcKey] = map[string]struct{}{}
		}
		areaSets[r.SrcKey][name] = struct{}{}
	}

	for act, set := range areaSets {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		c.areas[act] = names[0]
	}
	return nil
}

// loadParagraphs streams LegalParagraph.*.jsonl, resolves each paragraph's
// reference, and keeps only those that resolve. Results are sorted by key.
func (c *Corpus) loadParagraphs(seedDir string) error {
	files, err := filepath.Glob(filepath.Join(seedDir, "objects", "LegalParagraph.*.jsonl"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	var paras []paraMeta
	for _, f := range files {
		if err := scanObjects(f, func(o rawObjectProps) {
			if o.Type != "LegalParagraph" {
				return
			}
			ref := paragraphRef(o.Properties.LawRefID, o.Properties.ParagraphNum, o.Properties.SectionID)
			if ref == "" {
				return
			}
			if res := c.idx.ResolveRef(ref); !res.ActFound || len(res.Targets) == 0 {
				return // never emit an unresolvable reference
			}
			paras = append(paras, paraMeta{
				Key:          o.Key,
				LawRefID:     o.Properties.LawRefID,
				Name:         o.Properties.Name,
				SectionLabel: o.Properties.SectionLabel,
				SectionID:    o.Properties.SectionID,
				ParagraphNum: o.Properties.ParagraphNum,
				Title:        o.Properties.Title,
				Position:     o.Properties.Position,
			})
		}); err != nil {
			return err
		}
	}

	sort.Slice(paras, func(i, j int) bool { return paras[i].Key < paras[j].Key })
	c.paras = paras
	return nil
}

// scanObjects decodes a JSONL object file line by line, invoking fn for each
// record. It never holds more than one record in memory.
func scanObjects(path string, fn func(rawObjectProps)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20) // allow very long content lines
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var o rawObjectProps
		if err := json.Unmarshal(line, &o); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		fn(o)
	}
	return sc.Err()
}

// LoadParagraphContent streams the LegalParagraph files a second time and
// returns the content of exactly the paragraphs whose keys are in want. This
// keeps full-content memory bounded to the sampled set.
func LoadParagraphContent(seedDir string, want map[string]bool) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(seedDir, "objects", "LegalParagraph.*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	out := map[string]string{}
	for _, f := range files {
		if err := scanObjects(f, func(o rawObjectProps) {
			if o.Type != "LegalParagraph" || !want[o.Key] {
				return
			}
			out[o.Key] = o.Properties.Content
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// kindOfKey returns the corpus kind of an act key ("lov" or "forskrift").
func kindOfKey(key string) string {
	if i := strings.Index(key, "/"); i >= 0 {
		return key[:i]
	}
	return key
}

// paragraphRef builds the lovcite-resolvable reference for a paragraph. It
// prefers the §-number form when paragraphNum is present and falls back to the
// section-id form. An empty key returns "".
func paragraphRef(lawKey, paragraphNum, sectionID string) string {
	if lawKey == "" {
		return ""
	}
	if num := lovcite.NormalizeParagraphNum(paragraphNum); num != "" {
		return lawKey + "#§" + num
	}
	if sectionID != "" {
		return lawKey + "#" + sectionID
	}
	return lawKey
}
