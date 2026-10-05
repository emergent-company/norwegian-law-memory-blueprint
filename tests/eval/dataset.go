package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evaldata"
)

const defaultDataset = "evals/golden/core.jsonl"

// loadedItem pairs an Item with the file it was loaded from, for error reporting.
type loadedItem struct {
	Item evaldata.Item
	File string
}

// blueprintDir resolves the repository root: LAW_BLUEPRINT_DIR if set, otherwise
// derived from this source file's location (tests/eval/<file> -> root).
func blueprintDir() string {
	if d := os.Getenv("LAW_BLUEPRINT_DIR"); d != "" {
		return d
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// seedDir returns the committed seed directory (objects/ + relationships/).
func seedDir() string {
	return filepath.Join(blueprintDir(), "seed")
}

// resolveDatasets expands an EVAL_DATASET spec (comma-separated list of paths or
// globs, relative to the repo root or absolute) into a sorted, deduplicated list
// of JSONL file paths. Empty spec falls back to defaultDataset.
func resolveDatasets(spec string) ([]string, error) {
	if spec == "" {
		spec = defaultDataset
	}
	root := blueprintDir()
	seen := map[string]struct{}{}
	var files []string
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var matches []string
		if !filepath.IsAbs(part) {
			matches, _ = filepath.Glob(filepath.Join(root, part))
		}
		if len(matches) == 0 {
			matches, _ = filepath.Glob(part)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("EVAL_DATASET component %q matched no files", part)
		}
		for _, m := range matches {
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				files = append(files, m)
			}
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("EVAL_DATASET %q matched no files", spec)
	}
	sort.Strings(files)
	return files, nil
}

// loadItems reads every JSONL file in paths and returns the items paired with
// their source file. A decode error in any file aborts with a clear message.
func loadItems(paths []string) ([]loadedItem, error) {
	var out []loadedItem
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", p, err)
		}
		items, rerr := evaldata.ReadItemsJSONL(f)
		f.Close()
		if rerr != nil {
			return nil, fmt.Errorf("read %s: %w", p, rerr)
		}
		for _, it := range items {
			out = append(out, loadedItem{Item: it, File: p})
		}
	}
	return out, nil
}
