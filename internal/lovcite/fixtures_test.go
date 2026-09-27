package lovcite

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSeed writes a mini seed corpus to a temp dir and returns its path.
// objects and relationships map a filename to its JSONL content.
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

// standardObjects returns a small, self-contained corpus used by most tests.
func standardObjects() map[string]string {
	return map[string]string{
		"Law.jsonl": "" +
			`{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62","content":"## Kapittel 1\n\n### § 1-1 — Formål\n\nSe § 1-2 og § 9-9.\n\n### § 1-2 — Omfang\n\nJf. lov/1900-01-01-1 § 2.\n"}}` + "\n" +
			`{"type":"Law","key":"lov/1900-01-01-1","properties":{"ref_id":"lov/1900-01-01-1","content":"### § 1\n\nFørste.\n\n### § 2\n\nAndre.\n"}}` + "\n",
		"LegalParagraph.jsonl": "" +
			`{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"law_ref_id":"lov/2005-06-17-62","paragraph_num":"§ 1-1","section_id":"kapittel-1-paragraf-1","content":"Formålstekst."}}` + "\n" +
			`{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-2","properties":{"law_ref_id":"lov/2005-06-17-62","paragraph_num":"§ 1-2","section_id":"kapittel-1-paragraf-2","content":"Omfangstekst."}}` + "\n" +
			`{"type":"LegalParagraph","key":"lov/1900-01-01-1#paragraf-1","properties":{"law_ref_id":"lov/1900-01-01-1","paragraph_num":"§ 1","section_id":"paragraf-1","content":"Første."}}` + "\n" +
			`{"type":"LegalParagraph","key":"lov/1900-01-01-1#paragraf-2","properties":{"law_ref_id":"lov/1900-01-01-1","paragraph_num":"§ 2","section_id":"paragraf-2","content":"Andre."}}` + "\n",
	}
}

// standardRelationships returns HAS_PARAGRAPH links for standardObjects.
func standardRelationships() map[string]string {
	return map[string]string{
		"HAS_PARAGRAPH.jsonl": "" +
			`{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"position":1}}` + "\n" +
			`{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-2","properties":{"position":2}}` + "\n" +
			`{"type":"HAS_PARAGRAPH","srcKey":"lov/1900-01-01-1","dstKey":"lov/1900-01-01-1#paragraf-1","properties":{"position":1}}` + "\n" +
			`{"type":"HAS_PARAGRAPH","srcKey":"lov/1900-01-01-1","dstKey":"lov/1900-01-01-1#paragraf-2","properties":{"position":2}}` + "\n",
	}
}

// buildStandard builds the index over the standard fixture.
func buildStandard(t *testing.T, retainContent bool) *Index {
	t.Helper()
	dir := writeSeed(t, standardObjects(), standardRelationships())
	idx, err := Build(dir, BuildOptions{RetainContent: retainContent})
	if err != nil {
		t.Fatal(err)
	}
	return idx
}
