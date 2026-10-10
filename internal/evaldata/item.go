package evaldata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Item is one evaluation item emitted by the build step. Field order matches
// the documented JSONL schema exactly; keep it stable so downstream harness
// code can rely on the wire shape.
type Item struct {
	ID            string   `json:"id"`
	Course        string   `json:"course"`
	Semester      string   `json:"semester"`
	Question      string   `json:"question"`
	GoldPoints    []string `json:"gold_points"`
	GoldRefs      []string `json:"gold_refs"`
	LegalArea     string   `json:"legal_area"`
	Language      string   `json:"language"`
	Source        string   `json:"source"`
	SourceURL     string   `json:"source_url"`
	OppgaveURL    string   `json:"oppgave_url"`
	VeiledningURL string   `json:"veiledning_url"`
	Difficulty    string   `json:"difficulty"`
	Answerable    bool     `json:"answerable"`
	License       string   `json:"license"`
	NeedsCuration bool     `json:"needs_curation"`
}

// NewItem returns an Item pre-populated with the invariant defaults so the
// build step only has to fill in per-item fields.
func NewItem() Item {
	return Item{
		Source:        "uio",
		Difficulty:    "unknown",
		Answerable:    true,
		License:       "uio-public",
		NeedsCuration: true,
		GoldPoints:    []string{},
		GoldRefs:      []string{},
	}
}

// MarshalItem serialises a single item as one JSON line (no trailing newline).
func MarshalItem(it Item) ([]byte, error) {
	return json.Marshal(it)
}

// WriteItemsJSONL streams items to w as newline-delimited JSON.
func WriteItemsJSONL(w io.Writer, items []Item) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// ReadItemsJSONL reads newline-delimited items from r.
func ReadItemsJSONL(r io.Reader) ([]Item, error) {
	var out []Item
	dec := json.NewDecoder(r)
	for {
		var it Item
		if err := dec.Decode(&it); err == io.EOF {
			return out, nil
		} else if err != nil {
			return out, fmt.Errorf("decode item: %w", err)
		}
		out = append(out, it)
	}
}

// WriteItemsFile writes items to path as JSONL, creating parent directories.
func WriteItemsFile(path string, items []Item) error {
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteItemsJSONL(f, items)
}

// dirOf returns the directory component of path ("." when there is none).
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
