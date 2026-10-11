package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evaldata"
)

func runFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	source := fs.String("source", "uio", "source registry key (only \"uio\" is supported)")
	cacheDir := fs.String("cache-dir", "evals/cache", "cache root directory")
	limit := fs.Int("limit", 0, "max documents to fetch (0 = unlimited)")
	_ = fs.Parse(args)

	if *source != "uio" {
		fmt.Fprintf(os.Stderr, "fetch: unsupported source %q (only \"uio\")\n", *source)
		return 2
	}

	client := &http.Client{Timeout: 30 * time.Second}
	logf := func(f string, a ...interface{}) { fmt.Fprintf(os.Stderr, f+"\n", a...) }

	docs, err := evaldata.CrawlSource(evaldata.UIOSource, evaldata.CrawlOptions{
		Client: client,
		Limit:  *limit,
		Logf:   logf,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		return 1
	}

	srcDir := filepath.Join(*cacheDir, *source)
	manifestPath := filepath.Join(srcDir, "manifest.json")
	manifest, err := evaldata.LoadManifest(manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		return 1
	}

	downloaded := 0
	skipped := 0
	for _, d := range docs {
		filename := evaldata.Filename(d.URL)
		dest := evaldata.CachePath(*cacheDir, *source, d.Course, d.Semester, filename)
		rel := evaldata.RelCachePath(d.Course, d.Semester, filename)

		// Idempotency: skip a URL already cached with a matching hash.
		if existing, ok := findEntry(manifest, d.URL); ok {
			if existing.SHA256 != "" {
				if hash, err := evaldata.FileSHA256(dest); err == nil && hash == existing.SHA256 {
					skipped++
					continue
				}
			}
		}

		res, err := evaldata.Download(client, d.URL, dest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fetch: download %s: %v\n", d.URL, err)
			continue
		}
		downloaded++

		entry := evaldata.ManifestEntry{
			URL:       d.URL,
			SHA256:    res.SHA256,
			Bytes:     res.Bytes,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Course:    d.Course,
			Semester:  d.Semester,
			Kind:      d.Kind,
			Language:  d.Language,
			LocalPath: rel,
			SourceURL: d.SourceURL,
		}
		upsertEntry(manifest, entry)
		fmt.Fprintf(os.Stderr, "fetched %s %s %s %s (%d bytes)\n", d.Course, d.Semester, d.Kind, filename, res.Bytes)
	}

	if err := evaldata.SaveManifest(manifestPath, manifest); err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		return 1
	}

	fmt.Printf("discovered: %d documents\n", len(docs))
	fmt.Printf("downloaded: %d\n", downloaded)
	fmt.Printf("cached:     %d\n", skipped)
	fmt.Printf("manifest:   %s\n", manifestPath)
	return 0
}

func findEntry(m *evaldata.Manifest, url string) (evaldata.ManifestEntry, bool) {
	for _, e := range m.Entries {
		if e.URL == url {
			return e, true
		}
	}
	return evaldata.ManifestEntry{}, false
}

func upsertEntry(m *evaldata.Manifest, entry evaldata.ManifestEntry) {
	for i, e := range m.Entries {
		if e.URL == entry.URL {
			m.Entries[i] = entry
			return
		}
	}
	m.Entries = append(m.Entries, entry)
}
