package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	// manifestVersion is the schema version of seed/manifest.json. Bump when the
	// manifest shape changes.
	manifestVersion = 1
	// parserVersion identifies the parser/source-hash pipeline that produced the
	// manifest. Bump when parseDocument or the source-hash computation changes
	// materially, so a manifest built by an older parser is distinguishable.
	parserVersion = "1"
)

// sourceArchive records one source archive file used for the export. mtime is
// deliberately NOT stored: including it (or any timestamp) would make two dumps
// over identical inputs produce different bytes. mtime is logged to stderr at
// load time instead.
type sourceArchive struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// manifestDoc is the per-source-document change-detection entry. The hash is
// over the raw source XML bytes (see parseDocument), not derived/rendered props.
type manifestDoc struct {
	Type         string `json:"type"`
	SourceSHA256 string `json:"source_sha256"`
}

// manifestCounts records the exported record totals.
type manifestCounts struct {
	Objects       int            `json:"objects"`
	Relationships int            `json:"relationships"`
	ByType        map[string]int `json:"by_type"`
}

// seedManifest is the on-disk seed/manifest.json schema. It contains no
// timestamps, so two dumps over identical inputs are byte-identical.
type seedManifest struct {
	ManifestVersion int                    `json:"manifest_version"`
	ParserVersion   string                 `json:"parser_version"`
	Dataset         string                 `json:"dataset"`
	SourceArchives  []sourceArchive        `json:"source_archives"`
	Documents       map[string]manifestDoc `json:"documents"`
	Counts          manifestCounts         `json:"counts"`
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// hashFile returns the SHA-256 (hex) and byte size of path.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// buildManifest assembles a seedManifest from the source docs and archives. The
// documents map is keyed by ref_id; each hash is the source-bytes hash. A doc
// with no hash (old cache written by a pre-hash binary) is recorded as
// "unknown" and a single warning is emitted.
func buildManifest(docs []LovDoc, archives []sourceArchive, dataset string, objCount, relCount int, objByType map[string]int) seedManifest {
	m := seedManifest{
		ManifestVersion: manifestVersion,
		ParserVersion:   parserVersion,
		Dataset:         dataset,
		Documents:       make(map[string]manifestDoc, len(docs)),
		Counts: manifestCounts{
			Objects:       objCount,
			Relationships: relCount,
			ByType:        objByType,
		},
	}
	unknown := 0
	for _, d := range docs {
		h := d.SourceSHA256
		if h == "" {
			unknown++
			h = "unknown"
		}
		m.Documents[d.RefID] = manifestDoc{Type: d.DocType, SourceSHA256: h}
	}
	if unknown > 0 {
		fmt.Fprintf(os.Stderr, "  [manifest] WARN: %d source document(s) have no source hash (old cache?) — recorded as \"unknown\"\n", unknown)
	}

	// Deterministic order for source_archives regardless of load order.
	archCopy := append([]sourceArchive(nil), archives...)
	sort.Slice(archCopy, func(i, j int) bool { return archCopy[i].Name < archCopy[j].Name })
	m.SourceArchives = archCopy
	return m
}

// loadManifest reads an existing manifest. ok=false when it does not exist.
func loadManifest(path string) (seedManifest, bool, error) {
	var m seedManifest
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, false, nil
		}
		return m, false, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, false, err
	}
	return m, true, nil
}

// writeManifest writes the manifest as stable, indented JSON. Go's json package
// sorts map keys, so `documents` and `counts.by_type` are deterministic. A
// trailing newline is appended.
func writeManifest(path string, m seedManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

// manifestChanges holds the result of diffing two manifests' document sets.
type manifestChanges struct {
	Added       int
	Changed     int
	Removed     int
	Unchanged   int
	ChangedRefs []string
}

// diffManifests compares the per-document hashes of old vs new. added/removed
// are ref_ids present in only one side; changed are ref_ids present in both with
// a differing source_sha256; unchanged are identical on both sides. changed refs
// are sorted for deterministic logging.
func diffManifests(old, new seedManifest) manifestChanges {
	var c manifestChanges
	for ref, nd := range new.Documents {
		od, ok := old.Documents[ref]
		switch {
		case !ok:
			c.Added++
		case od.SourceSHA256 != nd.SourceSHA256:
			c.Changed++
			c.ChangedRefs = append(c.ChangedRefs, ref)
		default:
			c.Unchanged++
		}
	}
	for ref := range old.Documents {
		if _, ok := new.Documents[ref]; !ok {
			c.Removed++
		}
	}
	sort.Strings(c.ChangedRefs)
	return c
}
