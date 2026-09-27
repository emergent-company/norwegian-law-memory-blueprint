package lovcheck

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSeed writes a mini seed corpus (objects/ + relationships/) to a temp dir
// and returns the dir path.
func writeSeed(t *testing.T, objects, relationships map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	objDir := filepath.Join(dir, "objects")
	relDir := filepath.Join(dir, "relationships")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range objects {
		if err := os.WriteFile(filepath.Join(objDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range relationships {
		if err := os.WriteFile(filepath.Join(relDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runCheck writes the fixture, optionally a manifest and a schema, then runs
// Check with a generous sample budget so all violations are visible to tests.
func runCheck(t *testing.T, objects, relationships map[string]string, manifest, schema string) Report {
	t.Helper()
	dir := writeSeed(t, objects, relationships)
	opts := Options{SeedDir: dir, Samples: 1000, MaxYear: 2031}

	if manifest != "" {
		p := filepath.Join(dir, "manifest.json")
		if err := os.WriteFile(p, []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		opts.Manifest = p
	}
	// Explicit schema path: point at a real file when given, otherwise at a
	// path that does not exist so declared-set detection is cleanly skipped.
	schemaPath := filepath.Join(dir, "nonexistent-schema.yaml")
	if schema != "" {
		schemaPath = filepath.Join(dir, "schema.yaml")
		if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opts.Schema = schemaPath

	report, err := Check(opts)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// countFor returns the violation count for a check id, or -1 if unknown.
func countFor(r Report, id string) int {
	for _, c := range r.Checks {
		if c.ID == id {
			return c.Count
		}
	}
	return -1
}

// baseObjects is a minimal, fully-consistent corpus with zero violations.
func baseObjects() map[string]string {
	return map[string]string{
		"Law.jsonl":            `{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62","date_of_publication":"2005-06-17"}}` + "\n",
		"LegalParagraph.jsonl": `{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"law_ref_id":"lov/2005-06-17-62","section_id":"kapittel-1-paragraf-1","paragraph_num":"§ 1-1"}}` + "\n",
	}
}

func baseRelationships() map[string]string {
	return map[string]string{
		"HAS_PARAGRAPH.jsonl": `{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"position":1}}` + "\n",
	}
}
