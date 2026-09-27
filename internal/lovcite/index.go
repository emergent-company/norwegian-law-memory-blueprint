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

// Paragraph is a single provision (LegalParagraph object) in the corpus.
type Paragraph struct {
	Key          string `json:"key"`           // e.g. "lov/1969-06-13-26#kapittel-1-paragraf-1"
	LawRefID     string `json:"law_ref_id"`    // parent Law/Regulation key, e.g. "lov/1969-06-13-26"
	ParagraphNum string `json:"paragraph_num"` // printed section number, e.g. "§ 1-1" (may be empty)
	SectionID    string `json:"section_id"`    // e.g. "kapittel-1-paragraf-1"
	Content      string `json:"-"`             // retained only when RetainContent is set
}

// BuildOptions controls what the in-memory index retains.
type BuildOptions struct {
	// RetainContent keeps the Markdown body of each paragraph so that
	// verify-quote can match text. Audit does not need it and sets it false
	// to avoid retaining ~200MB of duplicated strings.
	RetainContent bool
}

// Index is an in-memory lookup structure built from the committed seed corpus.
//
// It streams every object file line by line (using a json.Decoder, which has no
// fixed line-length limit) and never stores the full Markdown body of a
// Law/Regulation object — only the paragraph index.
type Index struct {
	// actKeys is the set of all Law/Regulation object keys. Used to test
	// whether an explicit act reference (lov/…, forskrift/…) exists.
	actKeys map[string]struct{}
	// docKind maps an act key to its kind: "lov" or "forskrift".
	docKind map[string]string
	// paragraphKeys is the set of all LegalParagraph object keys
	// (law_ref_id#section_id). It is the authoritative "endpoint" existence
	// check for a resolved provision.
	paragraphKeys map[string]struct{}
	// byNum maps law_ref_id -> normalized paragraph number -> paragraph.
	// Only paragraphs with a non-empty paragraph_num are present.
	byNum map[string]map[string]*Paragraph
	// bySection maps law_ref_id -> section_id -> paragraph. Every paragraph
	// is present (including those with an empty paragraph_num).
	bySection map[string]map[string]*Paragraph
	// children maps law_ref_id -> set of section_id, from the HAS_PARAGRAPH
	// relationships. Used as an independent endpoint confirmation.
	children map[string]map[string]struct{}
}

type rawObject struct {
	Type       string `json:"type"`
	Key        string `json:"key"`
	Properties struct {
		Content      string `json:"content"`
		RefID        string `json:"ref_id"`
		LawRefID     string `json:"law_ref_id"`
		ParagraphNum string `json:"paragraph_num"`
		SectionID    string `json:"section_id"`
	} `json:"properties"`
}

type rawRelationship struct {
	Type   string `json:"type"`
	SrcKey string `json:"srcKey"`
	DstKey string `json:"dstKey"`
}

// Build constructs the index from a seed directory containing objects/*.jsonl
// and relationships/*.jsonl. Files are processed in sorted order so that any
// diagnostic output is deterministic.
func Build(seedDir string, opts BuildOptions) (*Index, error) {
	idx := &Index{
		actKeys:       make(map[string]struct{}),
		docKind:       make(map[string]string),
		paragraphKeys: make(map[string]struct{}),
		byNum:         make(map[string]map[string]*Paragraph),
		bySection:     make(map[string]map[string]*Paragraph),
		children:      make(map[string]map[string]struct{}),
	}

	objFiles, err := filepath.Glob(filepath.Join(seedDir, "objects", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(objFiles)
	for _, f := range objFiles {
		if err := idx.scanObjects(f, opts); err != nil {
			return nil, err
		}
	}

	relFiles, err := filepath.Glob(filepath.Join(seedDir, "relationships", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(relFiles)
	for _, f := range relFiles {
		if err := idx.scanRelationships(f); err != nil {
			return nil, err
		}
	}

	return idx, nil
}

func (idx *Index) scanObjects(path string, opts BuildOptions) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for {
		var o rawObject
		if err := dec.Decode(&o); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		switch o.Type {
		case "Law", "Regulation":
			idx.actKeys[o.Key] = struct{}{}
			idx.docKind[o.Key] = kindOfKey(o.Key)
		case "LegalParagraph":
			p := &Paragraph{
				Key:          o.Key,
				LawRefID:     o.Properties.LawRefID,
				ParagraphNum: o.Properties.ParagraphNum,
				SectionID:    o.Properties.SectionID,
			}
			if opts.RetainContent {
				p.Content = o.Properties.Content
			}
			idx.paragraphKeys[p.Key] = struct{}{}
			idx.addParagraph(p)
		}
	}
}

func (idx *Index) addParagraph(p *Paragraph) {
	if idx.bySection[p.LawRefID] == nil {
		idx.bySection[p.LawRefID] = make(map[string]*Paragraph)
	}
	idx.bySection[p.LawRefID][p.SectionID] = p

	if p.ParagraphNum != "" {
		if idx.byNum[p.LawRefID] == nil {
			idx.byNum[p.LawRefID] = make(map[string]*Paragraph)
		}
		idx.byNum[p.LawRefID][NormalizeParagraphNum(p.ParagraphNum)] = p
	}
}

func (idx *Index) scanRelationships(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for {
		var r rawRelationship
		if err := dec.Decode(&r); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if r.Type != "HAS_PARAGRAPH" {
			continue
		}
		base, section := splitKey(r.DstKey)
		if base == "" || section == "" {
			continue
		}
		if idx.children[base] == nil {
			idx.children[base] = make(map[string]struct{})
		}
		idx.children[base][section] = struct{}{}
	}
}

// splitKey splits a LegalParagraph key "law_ref_id#section_id" into its two parts.
func splitKey(key string) (base, section string) {
	if i := strings.Index(key, "#"); i >= 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}

// kindOfKey returns the corpus kind of an act key ("lov" or "forskrift").
func kindOfKey(key string) string {
	if i := strings.Index(key, "/"); i >= 0 {
		return key[:i]
	}
	return key
}

// HasAct reports whether key is a known Law/Regulation object.
func (idx *Index) HasAct(key string) bool {
	_, ok := idx.actKeys[key]
	return ok
}

// HasChild reports whether sectionID is confirmed by a HAS_PARAGRAPH
// relationship as a child of actKey.
func (idx *Index) HasChild(actKey, sectionID string) bool {
	m, ok := idx.children[actKey]
	if !ok {
		return false
	}
	_, ok = m[sectionID]
	return ok
}

// ActCount returns the number of Law/Regulation objects in the index.
func (idx *Index) ActCount() int { return len(idx.actKeys) }

// ParagraphCount returns the number of LegalParagraph objects in the index.
func (idx *Index) ParagraphCount() int { return len(idx.paragraphKeys) }
