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
	manifestVersion = 5
	// parserVersion identifies the parser/source-hash pipeline that produced the
	// manifest. Bump when parseDocument or the source-hash computation changes
	// materially, so a manifest built by an older parser is distinguishable.
	parserVersion = "1"

	// Dataset-level provenance. NLOD-2.0 requires attribution, not per-record
	// provenance, so provenance lives here (per-record would mean ~144k-record
	// churn plus a schema change across every object type).
	manifestSource      = "Lovdata"
	manifestLicense     = "NLOD-2.0"
	manifestLicenseURL  = "https://data.norge.no/nlod/en/2.0"
	manifestAttribution = "Data from Lovdata (https://lovdata.no/), licensed under the Norwegian Licence for Open Government Data (NLOD) 2.0. The data has been parsed, restructured and rendered into a knowledge-graph seed; changes were made."
	manifestNoticeFile  = "NOTICE"
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

// manifestCoverage aggregates per-document token coverage for the coverage gate.
type manifestCoverage struct {
	Docs         int     `json:"docs"`
	Mean         float64 `json:"mean"`
	Min          float64 `json:"min"`
	DocsBelow099 int     `json:"docs_below_0_99"`
	DocsBelow095 int     `json:"docs_below_0_95"`
}

// manifestQuality records corpus-level data-quality anomalies so they are
// visible and trendable (e.g. Lovdata error placeholders ingested in place of a
// real body).
type manifestQuality struct {
	ContentUnavailable     int      `json:"content_unavailable"`
	ContentUnavailableRefs []string `json:"content_unavailable_refs"`
}

// manifestPreparatoryWorks records the anchor-derived forarbeid coverage (phase A:
// metadata-only, no full text).
type manifestPreparatoryWorks struct {
	Total        int            `json:"total"`
	WithFullText int            `json:"with_full_text"`
	ByType       map[string]int `json:"by_type"`
	ByEra        map[string]int `json:"by_era"`
}

// seedManifest is the on-disk seed/manifest.json schema. It contains no
// timestamps, so two dumps over identical inputs are byte-identical.
type seedManifest struct {
	ManifestVersion  int                      `json:"manifest_version"`
	ParserVersion    string                   `json:"parser_version"`
	Dataset          string                   `json:"dataset"`
	Source           string                   `json:"source"`
	License          string                   `json:"license"`
	LicenseURL       string                   `json:"license_url"`
	Attribution      string                   `json:"attribution"`
	ChangesMade      bool                     `json:"changes_made"`
	NoticeFile       string                   `json:"notice_file"`
	SourceArchives   []sourceArchive          `json:"source_archives"`
	Documents        map[string]manifestDoc   `json:"documents"`
	Counts           manifestCounts           `json:"counts"`
	Coverage         manifestCoverage         `json:"coverage"`
	Quality          manifestQuality          `json:"quality"`
	PreparatoryWorks manifestPreparatoryWorks `json:"preparatory_works"`
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
func buildManifest(docs []LovDoc, archives []sourceArchive, dataset string, objCount, relCount int, objByType map[string]int, cov coverageSummary, preps []prepWork) seedManifest {
	m := seedManifest{
		ManifestVersion: manifestVersion,
		ParserVersion:   parserVersion,
		Dataset:         dataset,
		Source:          manifestSource,
		License:         manifestLicense,
		LicenseURL:      manifestLicenseURL,
		Attribution:     manifestAttribution,
		ChangesMade:     true,
		NoticeFile:      manifestNoticeFile,
		Documents:       make(map[string]manifestDoc, len(docs)),
		Counts: manifestCounts{
			Objects:       objCount,
			Relationships: relCount,
			ByType:        objByType,
		},
		Coverage: manifestCoverage{
			Docs:         cov.Docs,
			Mean:         cov.Mean,
			Min:          cov.Min,
			DocsBelow099: cov.DocsBelow099,
			DocsBelow095: cov.DocsBelow095,
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

	// Quality: collect content-unavailable refids (sorted for byte stability).
	var unavailable []string
	for _, d := range docs {
		if d.ContentUnavailable {
			unavailable = append(unavailable, d.RefID)
		}
	}
	sort.Strings(unavailable)
	m.Quality = manifestQuality{
		ContentUnavailable:     len(unavailable),
		ContentUnavailableRefs: unavailable,
	}

	// Preparatory works summary.
	ps := summarizePreparatoryWorks(preps)
	m.PreparatoryWorks = manifestPreparatoryWorks{
		Total:        ps.Total,
		WithFullText: ps.WithFullText,
		ByType:       ps.ByType,
		ByEra:        ps.ByEra,
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
