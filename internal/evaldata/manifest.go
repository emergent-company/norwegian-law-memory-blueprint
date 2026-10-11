package evaldata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ManifestEntry records one fetched document. It is the unit of idempotency:
// fetch skips a URL already present with a matching SHA-256.
type ManifestEntry struct {
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	Timestamp string `json:"timestamp"` // RFC3339 UTC
	Course    string `json:"course"`
	Semester  string `json:"semester"`
	Kind      string `json:"kind"`     // "oppgave" | "veiledning"
	Language  string `json:"language"` // "nb" | "nn"
	LocalPath string `json:"local_path"`
	SourceURL string `json:"source_url"` // the oppgaver page it was found on
}

// Manifest is the persisted cache index at evals/cache/uio/manifest.json.
type Manifest struct {
	Entries []ManifestEntry `json:"entries"`
}

// LoadManifest reads a manifest file, returning an empty manifest when the
// file does not exist.
func LoadManifest(path string) (*Manifest, error) {
	m := &Manifest{Entries: []ManifestEntry{}}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(m); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	if m.Entries == nil {
		m.Entries = []ManifestEntry{}
	}
	return m, nil
}

// SaveManifest writes the manifest atomically (write-then-rename).
func SaveManifest(path string, m *Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// HasURL reports whether the manifest already records url with the given hash.
func (m *Manifest) HasURL(url, sha256 string) bool {
	for _, e := range m.Entries {
		if e.URL == url && e.SHA256 == sha256 {
			return true
		}
	}
	return false
}

// ByKey indexes entries by (course, semester, kind, language) -> entry, used
// by the build step to pair oppgave and veiledning. Later entries win.
func (m *Manifest) ByKey() map[string]ManifestEntry {
	out := make(map[string]ManifestEntry, len(m.Entries))
	for _, e := range m.Entries {
		out[docKey(e.Course, e.Semester, e.Kind, e.Language)] = e
	}
	return out
}

// docKey builds the grouping key used to pair documents.
func docKey(course, semester, kind, language string) string {
	return course + "\x00" + semester + "\x00" + kind + "\x00" + language
}
