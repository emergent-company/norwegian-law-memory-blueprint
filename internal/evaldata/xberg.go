package evaldata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
)

// DefaultXbergBaseURL is the base URL used when XbergConfig.BaseURL is empty.
const DefaultXbergBaseURL = "http://localhost:8000"

// DefaultXbergTimeoutMs is the request timeout used when XbergConfig.TimeoutMs
// is <= 0.
const DefaultXbergTimeoutMs = 300000

// XbergConfig configures a call to the local xberg document-extraction service
// (PDF/DOCX/images -> text over HTTP).
type XbergConfig struct {
	BaseURL     string // service base URL; empty -> DefaultXbergBaseURL
	TimeoutMs   int    // per-request timeout in milliseconds; <=0 -> DefaultXbergTimeoutMs
	ForceOCR    bool   // request forced OCR
	OCRLanguage string // OCR language(s), "+"-separated (e.g. "nor" or "nor+eng")
	OCRBackend  string // OCR backend name (e.g. "tesseract")
}

// XbergFile is one file (content + filename) sent to the xberg /extract
// endpoint as a "files" part.
type XbergFile struct {
	Content  []byte
	Filename string
}

// xbergRequestConfig is the optional "config" JSON field sent to the xberg
// /extract endpoint. force_ocr is omitted when false; ocr is omitted when no
// backend or language is present.
type xbergRequestConfig struct {
	ForceOCR bool      `json:"force_ocr,omitempty"`
	OCR      *xbergOCR `json:"ocr,omitempty"`
}

// xbergOCR is the nested OCR config within xbergRequestConfig.
type xbergOCR struct {
	Backend  string   `json:"backend,omitempty"`
	Language []string `json:"language,omitempty"`
}

// xbergResponse is the 2xx JSON envelope returned by /extract.
type xbergResponse struct {
	Results []xbergResult  `json:"results"`
	Errors  []xbergError   `json:"errors"`
	Summary map[string]any `json:"summary"`
}

// xbergResult is one extracted document within the envelope.
type xbergResult struct {
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
	Tables   []any          `json:"tables"`
	Images   []any          `json:"images"`
}

// xbergError is one error entry in the envelope's errors array.
type xbergError struct {
	Message string `json:"message"`
	Error   string `json:"error"`
	File    string `json:"file"`
}

// XbergExtractText sends content to the xberg /extract endpoint and returns the
// extracted text (results[0].content). It never panics; all transport, status,
// and decode failures are returned as errors.
func XbergExtractText(ctx context.Context, content []byte, filename string, cfg XbergConfig) (string, error) {
	contents, err := XbergExtractFiles(ctx, []XbergFile{{Content: content, Filename: filename}}, cfg)
	if err != nil {
		return "", err
	}
	return contents[0], nil
}

// XbergExtractFiles sends files to the xberg /extract endpoint — one "files"
// part per XbergFile, in order, each with the Content-Type derived from its
// filename — and returns results[i].content in response order. It returns the
// contents actually received even if the response count differs from len(files)
// (the caller is responsible for joining). It never panics; transport, status,
// and decode failures are returned as errors.
func XbergExtractFiles(ctx context.Context, files []XbergFile, cfg XbergConfig) ([]string, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultXbergBaseURL
	}
	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = DefaultXbergTimeoutMs
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	for _, f := range files {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, f.Filename))
		h.Set("Content-Type", mimeForFilename(f.Filename))
		fw, err := mw.CreatePart(h)
		if err != nil {
			return nil, fmt.Errorf("xberg: create file part: %w", err)
		}
		if _, err := fw.Write(f.Content); err != nil {
			return nil, fmt.Errorf("xberg: write file part: %w", err)
		}
	}

	if cfgJSON := buildXbergConfigJSON(cfg); cfgJSON != "" {
		if err := mw.WriteField("config", cfgJSON); err != nil {
			return nil, fmt.Errorf("xberg: write config field: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("xberg: close multipart body: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/extract", &body)
	if err != nil {
		return nil, fmt.Errorf("xberg: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xberg extract %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xberg: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("xberg extract: HTTP %d: %s", resp.StatusCode, xbergErrorMessage(respBody))
	}

	var env xbergResponse
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("xberg: decode response: %w", err)
	}

	if len(env.Results) == 0 {
		if len(env.Errors) > 0 {
			return nil, fmt.Errorf("xberg: %s", joinXbergErrors(env.Errors))
		}
		return nil, fmt.Errorf("xberg returned empty results for %s", xbergFileNames(files))
	}

	contents := make([]string, 0, len(env.Results))
	for _, r := range env.Results {
		contents = append(contents, r.Content)
	}
	return contents, nil
}

// xbergFileNames joins the filenames of a set of XbergFile for error messages.
func xbergFileNames(files []XbergFile) string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Filename)
	}
	return strings.Join(names, ", ")
}

// buildXbergConfigJSON renders the optional "config" field per the xberg
// contract. It returns "" when no config is needed (force_ocr false and no
// backend or language). The OCR backend defaults to "tesseract" when a language
// is given without an explicit backend.
func buildXbergConfigJSON(cfg XbergConfig) string {
	lang := splitLanguages(cfg.OCRLanguage)
	backend := cfg.OCRBackend
	if backend == "" && len(lang) > 0 {
		backend = "tesseract"
	}

	rc := xbergRequestConfig{ForceOCR: cfg.ForceOCR}
	if backend != "" || len(lang) > 0 {
		rc.OCR = &xbergOCR{Backend: backend, Language: lang}
	}
	if !rc.ForceOCR && rc.OCR == nil {
		return ""
	}
	b, err := json.Marshal(rc)
	if err != nil {
		return "" // unreachable for these field types
	}
	return string(b)
}

// splitLanguages splits an OCR language spec on "+" into the canonical array,
// dropping empty segments ("nor" -> ["nor"]).
func splitLanguages(s string) []string {
	var out []string
	for _, part := range strings.Split(s, "+") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// joinXbergErrors joins each xberg error's message (or error) with "; ",
// prefixing "file: " when the file field is set.
func joinXbergErrors(errs []xbergError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		msg := e.Message
		if msg == "" {
			msg = e.Error
		}
		if msg == "" {
			continue
		}
		if e.File != "" {
			msg = "file: " + msg
		}
		parts = append(parts, msg)
	}
	return strings.Join(parts, "; ")
}

// xbergErrorMessage extracts a human-readable message from a non-2xx response
// body ({error|message|detail}), falling back to the raw body.
func xbergErrorMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return strings.TrimSpace(string(body))
	}
	parts := make([]string, 0, 3)
	for _, s := range []string{e.Error, e.Message, e.Detail} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(string(body))
	}
	return strings.Join(parts, ": ")
}

// mimeForFilename maps a filename extension to the Content-Type used for the
// multipart file part.
func mimeForFilename(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".tif", ".tiff":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}
