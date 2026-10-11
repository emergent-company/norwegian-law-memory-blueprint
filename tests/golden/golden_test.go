// Package golden pins a small set of well-known corpus anchors by sha256 so
// that any unintended drift in the committed seed is caught by `go test` and
// CI. It is fully offline: it reads only tests/golden/manifest.json and the
// committed seed/objects/*.jsonl, no server or credentials.
//
// Run as a normal assertion:
//
//	go test ./tests/golden/ -v
//
// Re-pin after an intentional regeneration:
//
//	go test ./tests/golden/ -update
package golden

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// update, when true, recomputes every anchor's sha256 from the current seed and
// rewrites manifest.json (stable, key-sorted ordering) instead of asserting.
var update = flag.Bool("update", false, "recompute and rewrite manifest.json")

// anchor is one pinned field hash in the golden contract.
type anchor struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Field  string `json:"field"`
	SHA256 string `json:"sha256"`
	Ref    string `json:"ref"`
	Note   string `json:"note,omitempty"`
}

// objectLine is a single seed/objects/*.jsonl line.
type objectLine struct {
	Type       string                 `json:"type"`
	Key        string                 `json:"key"`
	Properties map[string]interface{} `json:"properties"`
}

// repoRoot resolves the repository root from this file's location
// (tests/golden/golden_test.go → ../..).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve repository root")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func manifestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "tests", "golden", "manifest.json")
}

func seedObjectsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "seed", "objects")
}

// indexSeed streams every seed/objects/*.jsonl file and indexes each object by
// its key. Lines are decoded independently; no server is involved.
func indexSeed(t *testing.T) map[string]objectLine {
	t.Helper()
	entries, err := os.ReadDir(seedObjectsDir(t))
	if err != nil {
		t.Fatalf("read seed objects dir: %v", err)
	}
	idx := make(map[string]objectLine)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		f, err := os.Open(filepath.Join(seedObjectsDir(t), e.Name()))
		if err != nil {
			t.Fatalf("open %s: %v", e.Name(), err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024) // directive content lines are large
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var o objectLine
			if err := json.Unmarshal(line, &o); err != nil {
				f.Close()
				t.Fatalf("decode %s: %v", e.Name(), err)
			}
			idx[o.Key] = o
		}
		f.Close()
		if err := sc.Err(); err != nil {
			t.Fatalf("scan %s: %v", e.Name(), err)
		}
	}
	return idx
}

// hashValue hashes a field's value verbatim. The value must be a string (the
// decoded JSON string, no trimming/normalisation).
func hashValue(v interface{}) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field value is not a string (type %T)", v)
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:]), nil
}

func loadManifest(t *testing.T) []anchor {
	t.Helper()
	data, err := os.ReadFile(manifestPath(t))
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	var anchors []anchor
	if err := json.Unmarshal(data, &anchors); err != nil {
		t.Fatalf("decode manifest.json: %v", err)
	}
	return anchors
}

func writeManifest(t *testing.T, anchors []anchor) {
	t.Helper()
	data, err := json.MarshalIndent(anchors, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(manifestPath(t), data, 0o644); err != nil {
		t.Fatalf("write manifest.json: %v", err)
	}
}

func TestGoldenAnchors(t *testing.T) {
	anchors := loadManifest(t)
	if len(anchors) == 0 {
		t.Fatal("manifest.json is empty — a golden contract must pin at least one anchor")
	}

	idx := indexSeed(t)

	if *update {
		for i := range anchors {
			a := &anchors[i]
			h, err := resolveHash(idx, *a)
			if err != nil {
				t.Fatalf("anchor %q (%s): %v", a.Key, a.Ref, err)
			}
			a.SHA256 = h
		}
		sort.Slice(anchors, func(i, j int) bool {
			if anchors[i].Key != anchors[j].Key {
				return anchors[i].Key < anchors[j].Key
			}
			if anchors[i].Field != anchors[j].Field {
				return anchors[i].Field < anchors[j].Field
			}
			return anchors[i].Type < anchors[j].Type
		})
		writeManifest(t, anchors)
		return
	}

	for _, a := range anchors {
		obj, ok := idx[a.Key]
		if !ok {
			t.Errorf("golden anchor %q (%s): object not found in seed", a.Key, a.Ref)
			continue
		}
		if obj.Type != a.Type {
			t.Errorf("golden anchor %q (%s): manifest type %q != seed type %q", a.Key, a.Ref, a.Type, obj.Type)
			continue
		}
		v, ok := obj.Properties[a.Field]
		if !ok {
			t.Errorf("golden anchor %q (%s): field %q not present", a.Key, a.Ref, a.Field)
			continue
		}
		actual, err := hashValue(v)
		if err != nil {
			t.Errorf("golden anchor %q (%s): field %q: %v", a.Key, a.Ref, a.Field, err)
			continue
		}
		if actual != a.SHA256 {
			t.Errorf("golden anchor %q (%s) drifted:\n  field    %q\n  expected %s\n  actual   %s\n  run: go test ./tests/golden/ -update", a.Key, a.Ref, a.Field, a.SHA256, actual)
		}
	}
}

// resolveHash looks up an anchor's object/field in the index and returns the
// verbatim sha256 of the field value.
func resolveHash(idx map[string]objectLine, a anchor) (string, error) {
	obj, ok := idx[a.Key]
	if !ok {
		return "", fmt.Errorf("object not found in seed")
	}
	if obj.Type != a.Type {
		return "", fmt.Errorf("manifest type %q != seed type %q", a.Type, obj.Type)
	}
	v, ok := obj.Properties[a.Field]
	if !ok {
		return "", fmt.Errorf("field %q not present", a.Field)
	}
	return hashValue(v)
}
