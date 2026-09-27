package lovcheck

import (
	"encoding/json"
	"os"
)

// manifestFile models the seed/manifest.json produced by the seeder.
type manifestFile struct {
	Counts manifestCounts `json:"counts"`
}

// manifestCounts holds the expected corpus totals recorded at seed time.
type manifestCounts struct {
	Objects       int            `json:"objects"`
	Relationships int            `json:"relationships"`
	ByType        map[string]int `json:"by_type"`
}

// LoadManifest reads the seed manifest. A missing file is reported as a
// distinct error (os.IsNotExist) so the caller can skip V8 gracefully.
func LoadManifest(path string) (manifestCounts, error) {
	var mf manifestFile
	f, err := os.Open(path)
	if err != nil {
		return manifestCounts{}, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&mf); err != nil {
		return manifestCounts{}, err
	}
	return mf.Counts, nil
}
