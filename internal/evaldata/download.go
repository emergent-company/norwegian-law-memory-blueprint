package evaldata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DownloadResult reports the outcome of downloading one document.
type DownloadResult struct {
	SHA256 string
	Bytes  int64
}

// Download fetches rawURL to destPath, creating parent directories. It returns
// the SHA-256 and byte count of the saved file.
func Download(client *http.Client, rawURL, destPath string) (DownloadResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DownloadResult{}, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return DownloadResult{}, err
	}
	// Download to a temp file then rename so a partial write never corrupts
	// the cache.
	tmp := destPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return DownloadResult{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return DownloadResult{}, err
	}
	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return DownloadResult{}, err
	}
	return DownloadResult{SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: n}, nil
}

// CachePath returns the on-disk path for a document inside the source cache.
// Layout: <cacheDir>/<source>/<course>/<semester>/<filename>.
func CachePath(cacheDir, source, course, semester, filename string) string {
	return filepath.Join(cacheDir, source, sanitize(course), sanitize(semester), sanitize(filename))
}

// RelCachePath returns the manifest-relative path (course/semester/filename)
// stored in the manifest, matching CachePath below the source dir.
func RelCachePath(course, semester, filename string) string {
	return filepath.ToSlash(filepath.Join(sanitize(course), sanitize(semester), sanitize(filename)))
}

// FileSHA256 returns the SHA-256 hex digest of a file's contents.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sanitize strips path separators from a single path component.
func sanitize(s string) string {
	if s == "" {
		return "_"
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
