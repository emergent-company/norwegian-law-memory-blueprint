package main

// Norwegian Law Memory Blueprint Seed Filler
//
// Reconciles a live Memory project against the committed blueprint seed data
// (seed/objects/*.jsonl and seed/relationships/*.jsonl) and creates ONLY what is
// missing.
//
// Why: a bulk ingest into a remote project can fail part-way (e.g. the server DB
// crashing under load), leaving a large fraction of objects/relationships absent.
// Re-running the blueprints CLI installer is impractically slow (one ListObjects
// per object), and the seeder's upload path resolves every conflict with a
// per-key lookup, also slow for tens of thousands of already-present objects.
// This tool instead (a) enumerates the existing keys/edges in bulk and (b) bulk-
// creates only the gaps.
//
// Usage:
//   ./seedfill --server http://localhost:3012 --token <token> --project <id>
//   ./seedfill --server <url> --token <t> --project <id> --dry-run   # diff only
//
// Environment variables (all overridable by flags):
//   MEMORY_SERVER          server URL
//   MEMORY_PROJECT_TOKEN   project API token
//   MEMORY_PROJECT_ID      project ID
//   SEED_DIR               blueprint directory containing seed/ (default ".")

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

const (
	// listPageSize is the page size used when enumerating existing objects and
	// relationships. Large pages minimise the number of round-trips.
	listPageSize = 1000

	// setSep separates the components of a relationship set key so it is
	// collision-free.
	setSep = "\x00"

	// maxLoggedItemErrors caps how many per-item failure lines are printed; the
	// rest are counted silently.
	maxLoggedItemErrors = 5
)

// ─── Config ───────────────────────────────────────────────────────────────────

type config struct {
	serverURL         string
	token             string
	projectID         string
	dir               string
	workers           int
	batchSz           int
	dryRun            bool
	objectsOnly       bool
	relationshipsOnly bool
}

func envOr(envKey, defaultVal string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultVal
}

func envIntOr(envKey string, defaultVal int) int {
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func parseConfig() config {
	serverURL := flag.String("server", envOr("MEMORY_SERVER", ""), "Memory server URL (required)")
	token := flag.String("token", envOr("MEMORY_PROJECT_TOKEN", ""), "Project API token (required)")
	projectID := flag.String("project", envOr("MEMORY_PROJECT_ID", ""), "Project ID (required)")
	dir := flag.String("dir", envOr("SEED_DIR", "."), "Blueprint directory containing seed/ (default .)")
	workers := flag.Int("workers", envIntOr("SEEDFILL_WORKERS", 4), "Parallel bulk-create workers")
	batchSz := flag.Int("batch", envIntOr("SEEDFILL_BATCH", 100), "Batch size for bulk API calls (max 100)")
	dryRun := flag.Bool("dry-run", false, "Compute the diff and print the summary without writing anything")
	objectsOnly := flag.Bool("objects-only", false, "Only fill missing objects (skip relationships)")
	relationshipsOnly := flag.Bool("relationships-only", false, "Only fill missing relationships (skip object creation)")

	flag.Parse()

	sz := *batchSz
	if sz > 100 {
		log.Printf("Note: max batch size is 100; capping from %d", sz)
		sz = 100
	}
	if sz < 1 {
		sz = 1
	}
	w := *workers
	if w < 1 {
		w = 1
	}

	return config{
		serverURL:         *serverURL,
		token:             *token,
		projectID:         *projectID,
		dir:               *dir,
		workers:           w,
		batchSz:           sz,
		dryRun:            *dryRun,
		objectsOnly:       *objectsOnly,
		relationshipsOnly: *relationshipsOnly,
	}
}

func (c *config) validate() error {
	var missing []string
	if c.serverURL == "" {
		missing = append(missing, "--server / MEMORY_SERVER")
	}
	if c.token == "" {
		missing = append(missing, "--token / MEMORY_PROJECT_TOKEN")
	}
	if c.projectID == "" {
		missing = append(missing, "--project / MEMORY_PROJECT_ID")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required parameters: %s", strings.Join(missing, ", "))
	}
	if c.objectsOnly && c.relationshipsOnly {
		return fmt.Errorf("--objects-only and --relationships-only are mutually exclusive")
	}
	return nil
}

// ─── Seed record shapes (match cmd/seeder seedObjectLine/seedRelationshipLine) ─

type seedObjectLine struct {
	Type       string         `json:"type"`
	Key        string         `json:"key"`
	Properties map[string]any `json:"properties"`
}

type seedRelationshipLine struct {
	Type       string         `json:"type"`
	SrcKey     string         `json:"srcKey"`
	DstKey     string         `json:"dstKey"`
	Properties map[string]any `json:"properties,omitempty"`
}

// ─── Seed loading ─────────────────────────────────────────────────────────────

// loadSeedObjects reads all seed/objects/*.jsonl files in sorted filename order.
func loadSeedObjects(dir string) ([]seedObjectLine, error) {
	objDir := filepath.Join(dir, "seed", "objects")
	entries, err := os.ReadDir(objDir)
	if err != nil {
		return nil, fmt.Errorf("read seed/objects dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var out []seedObjectLine
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		recs, err := readObjectFile(filepath.Join(objDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, recs...)
	}
	return out, nil
}

// loadSeedRelationships reads all seed/relationships/*.jsonl files in sorted
// order, tolerating a missing directory (relationships are optional).
func loadSeedRelationships(dir string) ([]seedRelationshipLine, error) {
	relDir := filepath.Join(dir, "seed", "relationships")
	entries, err := os.ReadDir(relDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read seed/relationships dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var out []seedRelationshipLine
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		recs, err := readRelationshipFile(filepath.Join(relDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, recs...)
	}
	return out, nil
}

func readObjectFile(path string) ([]seedObjectLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []seedObjectLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec seedObjectLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("invalid object line: %w", err)
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

func readRelationshipFile(path string) ([]seedRelationshipLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []seedRelationshipLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec seedRelationshipLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("invalid relationship line: %w", err)
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// ─── Live enumeration ─────────────────────────────────────────────────────────

func collectObjectTypes(objects []seedObjectLine) []string {
	m := map[string]bool{}
	for _, o := range objects {
		m[o.Type] = true
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func collectRelTypes(rels []seedRelationshipLine) []string {
	m := map[string]bool{}
	for _, r := range rels {
		m[r.Type] = true
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// resolveEntityID returns the stable entity (canonical) ID for an object.
func resolveEntityID(o *graph.GraphObject) string {
	if o.EntityID != "" {
		return o.EntityID
	}
	if o.CanonicalID != "" {
		return o.CanonicalID
	}
	return o.ID
}

// normalizeID maps any object ID form (version or canonical) back to its
// canonical entity ID when known, falling back to the input.
func normalizeID(anyIDToEntity map[string]string, id string) string {
	if eid, ok := anyIDToEntity[id]; ok && eid != "" {
		return eid
	}
	return id
}

// enumerateObjects pages ListObjects per seed type until exhausted, returning the
// live key→entityID map plus an any-ID→entityID map used to canonicalise
// relationship endpoints. Pagination terminates on an empty page or a nil
// NextCursor.
func enumerateObjects(ctx context.Context, client *graph.Client, types []string) (map[string]string, map[string]string, error) {
	keyToID := make(map[string]string)
	anyIDToEntity := make(map[string]string)

	for _, typ := range types {
		cursor := ""
		page := 0
		for {
			opts := &graph.ListObjectsOptions{Type: typ, Limit: listPageSize}
			if cursor != "" {
				opts.Cursor = cursor
			}
			var resp *graph.SearchObjectsResponse
			var err error
			for attempt := 1; ; attempt++ {
				resp, err = client.ListObjects(ctx, opts)
				if err == nil || !retryable(err) || attempt >= 6 {
					break
				}
				backoff := time.Duration(attempt) * 2 * time.Second
				log.Printf("  enumerate objects: type=%s page=%d error: %v - retry %d/5 in %s", typ, page+1, err, attempt, backoff)
				select {
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				case <-time.After(backoff):
				}
			}
			if err != nil {
				return nil, nil, fmt.Errorf("ListObjects(%s): %w", typ, err)
			}
			page++
			for _, o := range resp.Items {
				eid := resolveEntityID(o)
				if o.Key != nil && *o.Key != "" {
					keyToID[*o.Key] = eid
				}
				anyIDToEntity[eid] = eid
				if o.ID != "" {
					anyIDToEntity[o.ID] = eid
				}
				if o.CanonicalID != "" {
					anyIDToEntity[o.CanonicalID] = eid
				}
				if o.VersionID != "" {
					anyIDToEntity[o.VersionID] = eid
				}
				if o.EntityID != "" {
					anyIDToEntity[o.EntityID] = eid
				}
			}
			if page%10 == 0 {
				log.Printf("  enumerate objects: type=%s page=%d items=%d keys=%d", typ, page, len(resp.Items), len(keyToID))
			}
			if len(resp.Items) == 0 || resp.NextCursor == nil || *resp.NextCursor == "" {
				log.Printf("  enumerate objects: type=%s done pages=%d keys=%d", typ, page, len(keyToID))
				break
			}
			if *resp.NextCursor == cursor {
				log.Printf("  enumerate objects: type=%s cursor did not advance at page %d - stopping", typ, page)
				break
			}
			cursor = *resp.NextCursor
		}
	}
	return keyToID, anyIDToEntity, nil
}

// enumerateRelationships pages ListRelationships per seed type and returns the set
// of existing (type, srcEntityID, dstEntityID) triples so re-runs stay idempotent.
func enumerateRelationships(ctx context.Context, client *graph.Client, types []string, anyIDToEntity map[string]string) (map[string]struct{}, error) {
	set := make(map[string]struct{})
	for _, typ := range types {
		cursor := ""
		page := 0
		for {
			opts := &graph.ListRelationshipsOptions{Type: typ, Limit: listPageSize}
			if cursor != "" {
				opts.Cursor = cursor
			}
			var resp *graph.SearchRelationshipsResponse
			var err error
			for attempt := 1; ; attempt++ {
				resp, err = client.ListRelationships(ctx, opts)
				if err == nil || !retryable(err) || attempt >= 6 {
					break
				}
				backoff := time.Duration(attempt) * 2 * time.Second
				log.Printf("  enumerate relationships: type=%s page=%d error: %v - retry %d/5 in %s", typ, page+1, err, attempt, backoff)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(backoff):
				}
			}
			if err != nil {
				return nil, fmt.Errorf("ListRelationships(%s): %w", typ, err)
			}
			page++
			for _, r := range resp.Items {
				src := normalizeID(anyIDToEntity, r.SrcID)
				dst := normalizeID(anyIDToEntity, r.DstID)
				set[typ+setSep+src+setSep+dst] = struct{}{}
			}
			if page%10 == 0 {
				log.Printf("  enumerate relationships: type=%s page=%d items=%d edges=%d", typ, page, len(resp.Items), len(set))
			}
			if len(resp.Items) == 0 || resp.NextCursor == nil || *resp.NextCursor == "" {
				log.Printf("  enumerate relationships: type=%s done pages=%d edges=%d", typ, page, len(set))
				break
			}
			if *resp.NextCursor == cursor {
				log.Printf("  enumerate relationships: type=%s cursor did not advance at page %d - stopping", typ, page)
				break
			}
			cursor = *resp.NextCursor
		}
	}
	return set, nil
}

// ─── Bulk creation ────────────────────────────────────────────────────────────

// retryable reports whether an error is worth retrying. The dev server returns
// 502/503/504 under bulk-write load, and transient connection errors surface as
// EOF/reset/timeout, so back off and retry those instead of dropping the batch.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{"502", "503", "504", "bad gateway", "service unavailable", "gateway timeout", "connection reset", "broken pipe", "eof", "timeout", "deadline exceeded", "temporarily unavailable"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// bulkCreateObjects bulk-creates objects in batches across workers, retrying a
// failed batch once. On a per-item conflict it resolves the existing object's ID
// via a key lookup (so it is neither "created" nor "failed"). Returns counts.
func bulkCreateObjects(ctx context.Context, client *graph.Client, cfg config, items []graph.CreateObjectRequest, keyToID map[string]string) (created, failed int) {
	if len(items) == 0 {
		return 0, 0
	}

	type workItem struct {
		idx   int
		batch []graph.CreateObjectRequest
	}
	type result struct {
		batch []graph.CreateObjectRequest
		res   *graph.BulkCreateObjectsResponse
	}

	var batches []workItem
	for i := 0; i < len(items); i += cfg.batchSz {
		end := i + cfg.batchSz
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, workItem{idx: len(batches), batch: items[i:end]})
	}

	work := make(chan workItem, cfg.workers*2)
	results := make(chan result, cfg.workers*2)

	var wg sync.WaitGroup
	for i := 0; i < cfg.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wi := range work {
				if ctx.Err() != nil {
					return
				}
				res, err := client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: wi.batch})
				attempts := 0
				for err != nil && attempts < 4 {
					attempts++
					if !retryable(err) {
						break
					}
					backoff := time.Duration(attempts) * 2 * time.Second
					log.Printf("  [objects] batch %d error: %v - retry %d/4 in %s", wi.idx, err, attempts, backoff)
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					res, err = client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: wi.batch})
				}
				if err != nil {
					log.Printf("  [objects] batch %d failed permanently: %v", wi.idx, err)
					results <- result{wi.batch, nil}
					continue
				}
				results <- result{wi.batch, res}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	go func() {
		for _, wi := range batches {
			if ctx.Err() != nil {
				break
			}
			work <- wi
		}
		close(work)
	}()

	var createdCount, failedCount atomic.Int64
	type conflictKey struct{ typ, key string }
	var conflicts []conflictKey
	var mu sync.Mutex

	for r := range results {
		if r.res == nil {
			failedCount.Add(int64(len(r.batch)))
			continue
		}
		for i, item := range r.res.Results {
			keyPtr := r.batch[i].Key
			if keyPtr == nil {
				continue
			}
			key := *keyPtr
			switch {
			case item.Object != nil:
				mu.Lock()
				keyToID[key] = resolveEntityID(item.Object)
				mu.Unlock()
				createdCount.Add(1)
			case item.Error != nil && strings.Contains(*item.Error, "conflict"):
				mu.Lock()
				conflicts = append(conflicts, conflictKey{r.batch[i].Type, key})
				mu.Unlock()
			case item.Error != nil:
				log.Printf("  [objects] error key=%s: %s", key, *item.Error)
				failedCount.Add(1)
			default:
				failedCount.Add(1)
			}
		}
	}

	if len(conflicts) > 0 {
		log.Printf("  Resolving %d conflicting keys by lookup...", len(conflicts))
		sem := make(chan struct{}, cfg.workers)
		var cwg sync.WaitGroup
		for _, ck := range conflicts {
			sem <- struct{}{}
			cwg.Add(1)
			go func(typ, key string) {
				defer cwg.Done()
				defer func() { <-sem }()
				resp, err := client.ListObjects(ctx, &graph.ListObjectsOptions{Type: typ, Key: key, Limit: 1})
				if err == nil && resp != nil && len(resp.Items) > 0 {
					mu.Lock()
					keyToID[key] = resolveEntityID(resp.Items[0])
					mu.Unlock()
				}
			}(ck.typ, ck.key)
		}
		cwg.Wait()
	}

	return int(createdCount.Load()), int(failedCount.Load())
}

// bulkCreateRelationships bulk-creates relationships in batches across workers,
// retrying a failed batch once. Returns created/failed counts.
func bulkCreateRelationships(ctx context.Context, client *graph.Client, cfg config, items []graph.CreateRelationshipRequest) (created, failed int) {
	if len(items) == 0 {
		return 0, 0
	}

	type workItem struct {
		idx   int
		batch []graph.CreateRelationshipRequest
	}
	type result struct {
		batch []graph.CreateRelationshipRequest
		res   *graph.BulkCreateRelationshipsResponse
	}

	var batches []workItem
	for i := 0; i < len(items); i += cfg.batchSz {
		end := i + cfg.batchSz
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, workItem{idx: len(batches), batch: items[i:end]})
	}

	work := make(chan workItem, cfg.workers*2)
	results := make(chan result, cfg.workers*2)

	var wg sync.WaitGroup
	for i := 0; i < cfg.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wi := range work {
				if ctx.Err() != nil {
					return
				}
				res, err := client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				attempts := 0
				for err != nil && attempts < 4 {
					attempts++
					if !retryable(err) {
						break
					}
					backoff := time.Duration(attempts) * 2 * time.Second
					log.Printf("  [rels] batch %d error: %v - retry %d/4 in %s", wi.idx, err, attempts, backoff)
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					res, err = client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				}
				if err != nil {
					log.Printf("  [rels] batch %d failed permanently: %v", wi.idx, err)
					results <- result{wi.batch, nil}
					continue
				}
				results <- result{wi.batch, res}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	go func() {
		for _, wi := range batches {
			if ctx.Err() != nil {
				break
			}
			work <- wi
		}
		close(work)
	}()

	var createdCount, failedCount atomic.Int64
	var logged atomic.Int64

	for r := range results {
		if r.res == nil {
			failedCount.Add(int64(len(r.batch)))
			continue
		}
		for _, item := range r.res.Results {
			if item.Success {
				createdCount.Add(1)
				continue
			}
			failedCount.Add(1)
			if item.Error != nil && logged.Add(1) <= maxLoggedItemErrors {
				log.Printf("  [rels] item error: %s", *item.Error)
			}
		}
	}

	return int(createdCount.Load()), int(failedCount.Load())
}

// ─── Main flow ────────────────────────────────────────────────────────────────

func main() {
	cfg := parseConfig()
	if err := cfg.validate(); err != nil {
		log.Fatalf("config error: %v", err)
	}

	start := time.Now()
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	client := graph.NewClient(httpClient, cfg.serverURL, auth.NewAPITokenProvider(cfg.token), "", cfg.projectID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %s — cancelling...", sig)
		cancel()
	}()

	objects, err := loadSeedObjects(cfg.dir)
	if err != nil {
		log.Fatalf("load seed objects: %v", err)
	}
	rels, err := loadSeedRelationships(cfg.dir)
	if err != nil {
		log.Fatalf("load seed relationships: %v", err)
	}
	log.Printf("Seed: %d objects, %d relationships in %s", len(objects), len(rels), cfg.dir)

	// 1. Enumerate live objects (needed for object diff and endpoint resolution).
	objTypes := collectObjectTypes(objects)
	keyToID, anyIDToEntity, err := enumerateObjects(ctx, client, objTypes)
	if err != nil {
		log.Fatalf("enumerate objects: %v", err)
	}
	log.Printf("Live objects enumerated: %d", len(keyToID))

	// 2. Enumerate live relationships (needed to skip existing edges; idempotent
	//    re-runs). Skipped entirely under --objects-only.
	var liveRels map[string]struct{}
	if !cfg.objectsOnly {
		relTypes := collectRelTypes(rels)
		liveRels, err = enumerateRelationships(ctx, client, relTypes, anyIDToEntity)
		if err != nil {
			log.Fatalf("enumerate relationships: %v", err)
		}
		log.Printf("Live relationships enumerated: %d", len(liveRels))
	}

	// 3. Compute and (unless dry-run / relationships-only) fill missing objects.
	var objCreated, objFailed int
	if !cfg.relationshipsOnly {
		var missing []graph.CreateObjectRequest
		for _, o := range objects {
			if keyToID[o.Key] == "" {
				key := o.Key
				missing = append(missing, graph.CreateObjectRequest{Type: o.Type, Key: &key, Properties: o.Properties})
			}
		}
		log.Printf("Missing objects: %d / %d", len(missing), len(objects))
		if cfg.dryRun {
			log.Printf("  [dry-run] would create %d objects", len(missing))
		} else {
			log.Printf("  Creating %d objects in batches of %d with %d workers...", len(missing), cfg.batchSz, cfg.workers)
			objCreated, objFailed = bulkCreateObjects(ctx, client, cfg, missing, keyToID)
		}
	}
	objFound := len(objects) - objCreated - objFailed

	// 4. Compute and (unless dry-run / objects-only) fill missing relationships.
	var relCreated, relFailed, relSkipped int
	if !cfg.objectsOnly {
		var missing []graph.CreateRelationshipRequest
		for _, r := range rels {
			if r.SrcKey == r.DstKey {
				relSkipped++
				continue
			}
			srcID := keyToID[r.SrcKey]
			dstID := keyToID[r.DstKey]
			if srcID == "" || dstID == "" || srcID == dstID {
				relSkipped++
				continue
			}
			if _, ok := liveRels[r.Type+setSep+srcID+setSep+dstID]; ok {
				continue // already exists
			}
			missing = append(missing, graph.CreateRelationshipRequest{Type: r.Type, SrcID: srcID, DstID: dstID, Properties: r.Properties})
		}
		log.Printf("Missing relationships: %d / %d", len(missing), len(rels))
		if cfg.dryRun {
			log.Printf("  [dry-run] would create %d relationships", len(missing))
		} else {
			log.Printf("  Creating %d relationships in batches of %d with %d workers...", len(missing), cfg.batchSz, cfg.workers)
			relCreated, relFailed = bulkCreateRelationships(ctx, client, cfg, missing)
		}
	}
	relFound := len(rels) - relCreated - relFailed - relSkipped

	// 5. Summary.
	log.Printf("──────────────────────────────────────────────")
	log.Printf("objects:      expected=%d found=%d created=%d failed=%d",
		len(objects), objFound, objCreated, objFailed)
	log.Printf("relationships: expected=%d found=%d created=%d failed=%d skipped_unresolvable=%d",
		len(rels), relFound, relCreated, relFailed, relSkipped)
	log.Printf("elapsed: %s", time.Since(start).Round(time.Millisecond))
}
