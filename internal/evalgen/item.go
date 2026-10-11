package evalgen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evaldata"
)

// ItemExt extends evaldata.Item with the marker fields the harness needs to
// separate item families. It embeds Item anonymously so the base fields flatten
// into the same JSON object (the wire shape of the base Item is preserved).
type ItemExt struct {
	evaldata.Item
	Task       string `json:"task,omitempty"`         // "retrieval" for retrieval items
	DocRef     string `json:"doc_ref,omitempty"`      // ground-truth paragraph key
	OutOfGraph bool   `json:"out_of_graph,omitempty"` // true when not in the statute/regulation graph
}

// NewItemExt returns an ItemExt pre-populated with the base Item defaults.
func NewItemExt() ItemExt {
	return ItemExt{Item: evaldata.NewItem()}
}

// MarshalItemExt serialises one extended item as a single JSON line.
func MarshalItemExt(it ItemExt) ([]byte, error) { return json.Marshal(it) }

// WriteItemsExtJSONL streams extended items to w as newline-delimited JSON.
func WriteItemsExtJSONL(w io.Writer, items []ItemExt) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// WriteItemsExtFile writes extended items to path as JSONL, creating parent
// directories as needed.
func WriteItemsExtFile(path string, items []ItemExt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteItemsExtJSONL(f, items)
}

// goldPointCap bounds the size of a single gold point (runes).
const goldPointCap = 600

// stripHeading removes the leading Markdown heading line ("### § 1-1 — ...")
// from a provision's content so the gold point is the body text, not a
// restatement of the section label.
func stripHeading(content string) string {
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "###") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		} else {
			s = ""
		}
	}
	return s
}

// capText truncates s to n runes, appending an ellipsis when truncated.
func capText(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// provisionGoldPoint reduces a provision's raw content to a single trimmed,
// capped gold point (the heading stripped, whitespace collapsed).
func provisionGoldPoint(content string) string {
	body := stripHeading(content)
	body = strings.Join(strings.Fields(body), " ")
	return capText(body, goldPointCap)
}

// actLabel returns the display label for an act: short_title when present, else
// the full name, else the raw key.
func actLabel(m actMeta, key string) string {
	if strings.TrimSpace(m.ShortTitle) != "" {
		return m.ShortTitle
	}
	if strings.TrimSpace(m.Name) != "" {
		return m.Name
	}
	return key
}

// sectionTitle returns the display title for a paragraph: the explicit title
// field when present, else the section_label.
func sectionTitle(p paraMeta) string {
	if strings.TrimSpace(p.Title) != "" {
		return p.Title
	}
	return p.SectionLabel
}

// syntheticQuestion templates the Q&A question: "Hva bestemmer <act> <label>?".
func syntheticQuestion(actLabel, sectionLabel string) string {
	return fmt.Sprintf("Hva bestemmer %s %s?", actLabel, sectionLabel)
}

// retrievalQuestion templates the retrieval question: "Hvilken bestemmelse i
// <act> regulerer <topic>?".
func retrievalQuestion(actLabel, topic string) string {
	return fmt.Sprintf("Hvilken bestemmelse i %s regulerer %s?", actLabel, topic)
}
