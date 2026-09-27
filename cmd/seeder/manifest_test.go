package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestManifestJSONDeterministic(t *testing.T) {
	docs := []LovDoc{
		{RefID: "lov/2005-06-17-62", DocType: "Law", SourceSHA256: "aaa"},
		{RefID: "lov/1999-01-01-1", DocType: "Law", SourceSHA256: "bbb"},
		{RefID: "forskrift/2020-01-01-1", DocType: "Regulation", SourceSHA256: "ccc"},
	}
	archives := []sourceArchive{
		{Name: "gjeldende-sentrale-forskrifter.tar.bz2", SHA256: "s2", Size: 222},
		{Name: "gjeldende-lover.tar.bz2", SHA256: "s1", Size: 111},
	}
	objByType := map[string]int{"Law": 2, "Regulation": 1}
	cov := coverageSummary{Docs: 3, Mean: 0.985, Min: 0.97, DocsBelow099: 1}

	m1 := buildManifest(docs, archives, "both", 3, 5, objByType, cov)
	m2 := buildManifest(docs, archives, "both", 3, 5, objByType, cov)

	b1, err := json.MarshalIndent(m1, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := json.MarshalIndent(m2, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("manifest not deterministic across builds:\n%s\nvs\n%s", b1, b2)
	}

	// Round-trip: write then load must reproduce the same JSON bytes.
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.json")
	if err := writeManifest(p, m1); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := loadManifest(p)
	if err != nil || !ok {
		t.Fatalf("loadManifest: ok=%v err=%v", ok, err)
	}
	b3, err := json.MarshalIndent(loaded, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b3) {
		t.Fatalf("round-trip mismatch:\n%s\nvs\n%s", b1, b3)
	}

	// The archives slice must be sorted by name regardless of input order.
	if len(loaded.SourceArchives) != 2 {
		t.Fatalf("SourceArchives len = %d, want 2", len(loaded.SourceArchives))
	}
	if loaded.SourceArchives[0].Name != "gjeldende-lover.tar.bz2" || loaded.SourceArchives[1].Name != "gjeldende-sentrale-forskrifter.tar.bz2" {
		t.Fatalf("SourceArchives not sorted: %+v", loaded.SourceArchives)
	}
}

func TestManifestUnchangedCorpus(t *testing.T) {
	docs := []LovDoc{
		{RefID: "a", DocType: "Law", SourceSHA256: "h1"},
		{RefID: "b", DocType: "Law", SourceSHA256: "h2"},
	}
	old := buildManifest(docs, nil, "laws", 2, 0, nil, coverageSummary{})
	new := buildManifest(docs, nil, "laws", 2, 0, nil, coverageSummary{})
	ch := diffManifests(old, new)
	if ch.Added != 0 || ch.Changed != 0 || ch.Removed != 0 || ch.Unchanged != 2 {
		t.Fatalf("diff = %+v; want added=0 changed=0 removed=0 unchanged=2", ch)
	}
}

func TestManifestChangeDetection(t *testing.T) {
	docsOld := []LovDoc{
		{RefID: "a", DocType: "Law", SourceSHA256: "h1"},
		{RefID: "b", DocType: "Law", SourceSHA256: "h2"},
		{RefID: "c", DocType: "Regulation", SourceSHA256: "h3"},
	}
	docsNew := []LovDoc{
		{RefID: "a", DocType: "Law", SourceSHA256: "h1"},         // unchanged
		{RefID: "b", DocType: "Law", SourceSHA256: "h2-CHANGED"}, // changed
		{RefID: "d", DocType: "Regulation", SourceSHA256: "h4"},  // added
		// "c" removed
	}
	old := buildManifest(docsOld, nil, "both", 3, 0, nil, coverageSummary{})
	new := buildManifest(docsNew, nil, "both", 3, 0, nil, coverageSummary{})

	ch := diffManifests(old, new)
	if ch.Added != 1 || ch.Changed != 1 || ch.Removed != 1 || ch.Unchanged != 1 {
		t.Fatalf("diff = %+v; want added=1 changed=1 removed=1 unchanged=1", ch)
	}
	if len(ch.ChangedRefs) != 1 || ch.ChangedRefs[0] != "b" {
		t.Fatalf("ChangedRefs = %v; want [b]", ch.ChangedRefs)
	}
}

func TestManifestOldCacheUnknownHash(t *testing.T) {
	// A doc with an empty SourceSHA256 (old pre-hash cache) must be recorded as
	// "unknown" rather than crash.
	docs := []LovDoc{
		{RefID: "a", DocType: "Law", SourceSHA256: ""},
		{RefID: "b", DocType: "Law", SourceSHA256: "h2"},
	}
	m := buildManifest(docs, nil, "laws", 2, 0, nil, coverageSummary{})
	if m.Documents["a"].SourceSHA256 != "unknown" {
		t.Fatalf("empty hash not recorded as 'unknown': %+v", m.Documents["a"])
	}
	if m.Documents["b"].SourceSHA256 != "h2" {
		t.Fatalf("hash corrupted: %+v", m.Documents["b"])
	}
}
