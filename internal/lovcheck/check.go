package lovcheck

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultMaxViolations is the sentinel used when --max-violations is not given.
// It is the maximum int value, so `check` is report-only by default and only
// exits non-zero when the caller passes a concrete threshold that is exceeded.
const DefaultMaxViolations = int(^uint(0) >> 1)

// Violation is a single integrity problem found by one check.
type Violation struct {
	Check  string `json:"check"`         // check id, e.g. "V4"
	Detail string `json:"detail"`        // human-readable explanation
	Key    string `json:"key,omitempty"` // offending object/relationship key
}

// CheckResult is the outcome of a single check: an id, a count and up to K
// sample violations.
type CheckResult struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Count   int         `json:"count"`
	Samples []Violation `json:"samples"`
}

// Inventory is the deterministic corpus summary printed alongside the checks.
type Inventory struct {
	ObjectCountByType              map[string]int `json:"object_count_by_type"`
	RelationshipCountByType        map[string]int `json:"relationship_count_by_type"`
	DeclaredButUnusedRelationships []string       `json:"declared_but_unused_relationships,omitempty"`
	DeclaredButAbsentObjectTypes   []string       `json:"declared_but_absent_object_types,omitempty"`
}

// Report is the full deterministic result of a `lovcheck check` run.
type Report struct {
	Checks          []CheckResult `json:"checks"`
	Inventory       Inventory     `json:"inventory"`
	TotalViolations int           `json:"total_violations"`
	Notes           []string      `json:"notes,omitempty"`
}

// Options controls a check run.
type Options struct {
	SeedDir  string
	Manifest string // path to manifest.json ("" => try <seed>/manifest.json)
	Schema   string // path to the template-pack YAML ("" => "schemas/norwegian-law.yaml")
	Samples  int    // number of sample violations per check
	MaxYear  int    // date-sanity upper bound (0 => time.Now().Year()+5)
}

// checkDefs is the fixed, ordered list of checks. Order is stable for output.
var checkDefs = []struct{ id, name string }{
	{"V1", "duplicate object keys"},
	{"V2", "dangling relationship endpoint"},
	{"V3", "self-loop relationship"},
	{"V4", "LegalParagraph structural consistency"},
	{"V5", "HAS_PARAGRAPH consistency"},
	{"V6", "ref_id pattern"},
	{"V7", "date sanity"},
	{"V8", "count cross-check vs manifest"},
}

// accumulator collects the violations for one check.
type accumulator struct {
	violations []Violation
}

// rawObject is the minimal decode for the map-building pass.
type rawObject struct {
	Type       string `json:"type"`
	Key        string `json:"key"`
	Properties struct {
		LawRefID string `json:"law_ref_id"`
	} `json:"properties"`
}

// rawObjectProps decodes an object with its full properties kept raw for the
// per-field checks (V4/V6/V7).
type rawObjectProps struct {
	Type       string          `json:"type"`
	Key        string          `json:"key"`
	Properties json.RawMessage `json:"properties"`
}

// rawRelationship decodes a relationship line.
type rawRelationship struct {
	Type   string `json:"type"`
	SrcKey string `json:"srcKey"`
	DstKey string `json:"dstKey"`
}

// Check runs the full integrity pass and returns a deterministic Report. It
// never fails on violations — the caller applies the --max-violations policy.
func Check(opts Options) (Report, error) {
	maxYear := opts.MaxYear
	if maxYear == 0 {
		maxYear = time.Now().Year() + 5
	}

	acc := make([]accumulator, len(checkDefs))
	idx := map[string]int{}
	for i, c := range checkDefs {
		idx[c.id] = i
	}
	add := func(id, detail, key string) {
		i := idx[id]
		acc[i].violations = append(acc[i].violations, Violation{Check: id, Detail: detail, Key: key})
	}

	var report Report
	report.Inventory = Inventory{
		ObjectCountByType:       map[string]int{},
		RelationshipCountByType: map[string]int{},
	}

	if _, err := os.Stat(filepath.Join(opts.SeedDir, "objects")); err != nil {
		return report, fmt.Errorf("seed objects dir: %w", err)
	}

	// ---- Pass 1: objects — build the key set and per-type counts. ----
	keyType := map[string]string{}
	keyCount := map[string]int{}
	lawRegKeys := map[string]struct{}{}
	lpLawRefID := map[string]string{}

	objFiles, err := filepath.Glob(filepath.Join(opts.SeedDir, "objects", "*.jsonl"))
	if err != nil {
		return report, err
	}
	sort.Strings(objFiles)
	for _, path := range objFiles {
		if err := scanObjects(path, func(o rawObject) {
			keyType[o.Key] = o.Type
			keyCount[o.Key]++
			report.Inventory.ObjectCountByType[o.Type]++
			if o.Type == "Law" || o.Type == "Regulation" {
				lawRegKeys[o.Key] = struct{}{}
			}
			if o.Type == "LegalParagraph" {
				lpLawRefID[o.Key] = o.Properties.LawRefID
			}
		}); err != nil {
			return report, err
		}
	}

	// V1: duplicate object keys (computed from the occurrence count).
	for k, n := range keyCount {
		if n > 1 {
			add("V1", fmt.Sprintf("key emitted %d times", n), k)
		}
	}

	// ---- Pass 2: objects — field-level checks needing the full key set. ----
	for _, path := range objFiles {
		if err := scanObjectsProps(path, func(o rawObjectProps) {
			props, err := propsMap(o.Properties)
			if err != nil {
				// A malformed properties object is itself a V4-worthy structural
				// problem if it is a LegalParagraph; otherwise skip silently.
				if o.Type == "LegalParagraph" {
					add("V4", "unparseable properties", o.Key)
				}
				return
			}
			switch o.Type {
			case "LegalParagraph":
				checkLegalParagraph(o.Key, props, lawRegKeys, add)
			case "Law", "Regulation":
				checkRefID(o.Key, props, add)
				checkDates(o.Key, props, maxYear, add)
			case "EUDirective":
				checkDates(o.Key, props, maxYear, add)
			}
		}); err != nil {
			return report, err
		}
	}

	// ---- Pass 3: relationships. ----
	lpIncoming := map[string]struct{}{}
	relFiles, err := filepath.Glob(filepath.Join(opts.SeedDir, "relationships", "*.jsonl"))
	if err != nil {
		return report, err
	}
	sort.Strings(relFiles)
	for _, path := range relFiles {
		if err := scanRelationships(path, func(r rawRelationship) {
			report.Inventory.RelationshipCountByType[r.Type]++

			// V3: self-loop.
			if r.SrcKey == r.DstKey {
				add("V3", "relationship points to itself", r.SrcKey)
			}
			// V2: dangling endpoint.
			if _, ok := keyType[r.SrcKey]; !ok {
				add("V2", fmt.Sprintf("srcKey %q is not an object", r.SrcKey), r.SrcKey)
			}
			if _, ok := keyType[r.DstKey]; !ok {
				add("V2", fmt.Sprintf("dstKey %q is not an object", r.DstKey), r.DstKey)
			}

			// V5: HAS_PARAGRAPH consistency.
			if r.Type == "HAS_PARAGRAPH" {
				if keyType[r.DstKey] != "LegalParagraph" {
					add("V5", "HAS_PARAGRAPH dstKey is not a LegalParagraph", r.DstKey)
				} else {
					lpIncoming[r.DstKey] = struct{}{}
					if r.SrcKey != lpLawRefID[r.DstKey] {
						add("V5", fmt.Sprintf("HAS_PARAGRAPH srcKey %q != law_ref_id %q", r.SrcKey, lpLawRefID[r.DstKey]), r.DstKey)
					}
				}
			}
		}); err != nil {
			return report, err
		}
	}

	// V5: LegalParagraph with no incoming HAS_PARAGRAPH edge.
	for k := range lpLawRefID {
		if _, ok := lpIncoming[k]; !ok {
			add("V5", "LegalParagraph has no incoming HAS_PARAGRAPH edge", k)
		}
	}

	// ---- V8: count cross-check vs manifest. ----
	manifest, manifestPath, manifestErr := resolveManifest(opts)
	if manifestErr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("V8 skipped: %s (%v)", manifestPath, manifestErr))
	} else {
		checkCounts(manifest, report.Inventory, add)
		report.Notes = append(report.Notes,
			"V8 note: manifest has no per-relationship-type breakdown; only total relationship count checked")
	}

	// ---- Declared-but-unused (schema) inventory. ----
	schemaPath := opts.Schema
	if schemaPath == "" {
		schemaPath = "schemas/norwegian-law.yaml"
	}
	if sd, err := LoadSchemaDeclared(schemaPath); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("declared-set detection skipped: %s (%v)", schemaPath, err))
	} else {
		report.Inventory.DeclaredButUnusedRelationships = setDiff(sd.RelationshipTypes, keysOf(report.Inventory.RelationshipCountByType))
		report.Inventory.DeclaredButAbsentObjectTypes = setDiff(sd.ObjectTypes, keysOf(report.Inventory.ObjectCountByType))
	}

	// ---- Assemble results. ----
	total := 0
	for i, c := range checkDefs {
		n := len(acc[i].violations)
		total += n
		report.Checks = append(report.Checks, CheckResult{
			ID:      c.id,
			Name:    c.name,
			Count:   n,
			Samples: sampleViolations(acc[i].violations, opts.Samples),
		})
	}
	report.TotalViolations = total
	return report, nil
}

// ---- scan helpers ----

func scanObjects(path string, fn func(rawObject)) error {
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
		fn(o)
	}
}

func scanObjectsProps(path string, fn func(rawObjectProps)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var o rawObjectProps
		if err := dec.Decode(&o); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		fn(o)
	}
}

func scanRelationships(path string, fn func(rawRelationship)) error {
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
		fn(r)
	}
}

// ---- per-check logic ----

func propsMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// rawString extracts a string property, returning "" for absent/null/non-string.
func rawString(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// checkLegalParagraph implements V4.
func checkLegalParagraph(key string, props map[string]json.RawMessage, lawRegKeys map[string]struct{}, add func(id, detail, key string)) {
	lawRefID := rawString(props, "law_ref_id")
	sectionID := rawString(props, "section_id")
	switch {
	case lawRefID == "":
		add("V4", "empty law_ref_id", key)
	case sectionID == "":
		add("V4", "empty section_id", key)
	case key != lawRefID+"#"+sectionID:
		add("V4", fmt.Sprintf("key %q != law_ref_id#section_id %q", key, lawRefID+"#"+sectionID), key)
	case !isLawReg(lawRegKeys, lawRefID):
		add("V4", fmt.Sprintf("law_ref_id %q is not a Law/Regulation object", lawRefID), key)
	}
}

// checkRefID implements V6.
func checkRefID(key string, props map[string]json.RawMessage, add func(id, detail, key string)) {
	refID := rawString(props, "ref_id")
	if refID == "" {
		add("V6", "empty ref_id", key)
		return
	}
	if !refIDPattern.MatchString(refID) {
		add("V6", fmt.Sprintf("ref_id %q does not match lov|forskrift pattern", refID), key)
	}
}

// checkDates implements V7 across the declared date-typed properties.
func checkDates(key string, props map[string]json.RawMessage, maxYear int, add func(id, detail, key string)) {
	for _, f := range dateProperties {
		raw, ok := props[f]
		if !ok || string(raw) == "null" {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			add("V7", fmt.Sprintf("%s is not a string", f), key)
			continue
		}
		if ok, reason := classifyDate(s, maxYear); !ok {
			add("V7", fmt.Sprintf("%s=%q: %s", f, s, reason), key)
		}
	}
}

// checkCounts implements V8.
func checkCounts(manifest manifestCounts, inv Inventory, add func(id, detail, key string)) {
	actualObjects := 0
	for _, n := range inv.ObjectCountByType {
		actualObjects += n
	}
	actualRelationships := 0
	for _, n := range inv.RelationshipCountByType {
		actualRelationships += n
	}

	if manifest.Objects != actualObjects {
		add("V8", fmt.Sprintf("objects: manifest=%d actual=%d", manifest.Objects, actualObjects), "objects")
	}
	if manifest.Relationships != actualRelationships {
		add("V8", fmt.Sprintf("relationships: manifest=%d actual=%d", manifest.Relationships, actualRelationships), "relationships")
	}
	// Per-object-type counts, in both directions.
	for typ, want := range manifest.ByType {
		got := inv.ObjectCountByType[typ]
		if want != got {
			add("V8", fmt.Sprintf("object type %s: manifest=%d actual=%d", typ, want, got), typ)
		}
	}
	for typ := range inv.ObjectCountByType {
		if _, ok := manifest.ByType[typ]; !ok {
			add("V8", fmt.Sprintf("object type %s: not in manifest", typ), typ)
		}
	}
}

func resolveManifest(opts Options) (manifestCounts, string, error) {
	path := opts.Manifest
	if path == "" {
		path = filepath.Join(opts.SeedDir, "manifest.json")
	}
	mc, err := LoadManifest(path)
	return mc, path, err
}

// ---- small helpers ----

func isLawReg(lawRegKeys map[string]struct{}, key string) bool {
	_, ok := lawRegKeys[key]
	return ok
}

func keysOf(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setDiff returns the sorted elements of declared that are absent from emitted.
func setDiff(declared, emitted []string) []string {
	have := map[string]bool{}
	for _, e := range emitted {
		have[e] = true
	}
	var out []string
	for _, d := range declared {
		if !have[d] {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// sampleViolations returns up to n samples spread evenly across the input.
func sampleViolations(all []Violation, n int) []Violation {
	if n <= 0 || len(all) == 0 {
		return nil
	}
	if len(all) <= n {
		out := append([]Violation(nil), all...)
		return out
	}
	out := make([]Violation, 0, n)
	step := float64(len(all)) / float64(n)
	for i := 0; i < n; i++ {
		out = append(out, all[int(float64(i)*step)])
	}
	return out
}
