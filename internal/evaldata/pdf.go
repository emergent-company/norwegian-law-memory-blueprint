package evaldata

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	// OCRMinChars is the non-space character count below which a pdftotext
	// result is treated as a scanned (image-only) PDF and OCR is attempted.
	OCRMinChars = 200

	// DefaultOCRMaxPages caps how many pages get rendered + OCR'd per document,
	// bounding pathological runtime on long scanned papers.
	DefaultOCRMaxPages = 40
)

// ExtractOptions controls text extraction, including the OCR fallback and the
// optional xberg service backend.
type ExtractOptions struct {
	OCRMaxPages int                          // max pages to OCR (0 -> DefaultOCRMaxPages)
	NoOCR       bool                         // disable the OCR fallback entirely
	Converter   string                       // "" or "pdftotext" => local; "xberg" => service
	Xberg       *XbergConfig                 // xberg service config; used when Converter == "xberg"
	Logf        func(string, ...interface{}) // optional diagnostics sink
}

// ExtractResult is the outcome of ExtractText.
type ExtractResult struct {
	Text      string // extracted text (pdftotext output, or OCR when used)
	OCRUsed   bool   // whether the OCR fallback produced the text
	XbergUsed bool   // whether the xberg service produced the text
}

// PDFToText extracts the text of a PDF file using pdftotext (poppler-utils).
// It shells out rather than linking a Go PDF library, per the build contract.
func PDFToText(pdfPath string) (string, error) {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", fmt.Errorf("pdftotext not found in PATH: %w (install poppler-utils)", err)
	}
	cmd := exec.Command(bin, "-layout", pdfPath, "-")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext %s: %w", pdfPath, err)
	}
	return out.String(), nil
}

// isScanned reports whether extracted text is too short to be a text-layer PDF,
// i.e. an image-only scan.
func isScanned(text string) bool {
	n := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n < OCRMinChars
}

// ExtractText extracts PDF text, falling back to OCR when the pdftotext result
// is too short and OCR is enabled. It never hard-fails when OCR tools are
// absent: in that case it logs a warning and returns the pdftotext output.
//
// When Converter == "xberg" and Xberg is set, the document is sent to the xberg
// service instead. Text-layer PDFs are sent as-is; scanned (image-only) PDFs
// are rendered to PNGs and sent as images so xberg OCRs them cleanly. On any
// xberg failure it logs a warning and falls back to the local pdftotext (+OCR)
// path.
func ExtractText(pdfPath string, opts ExtractOptions) (ExtractResult, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}

	maxPages := opts.OCRMaxPages
	if maxPages <= 0 {
		maxPages = DefaultOCRMaxPages
	}

	if opts.Converter == "xberg" && opts.Xberg != nil {
		if res, ok := extractViaXberg(pdfPath, opts.Xberg, maxPages, logf); ok {
			return res, nil
		}
	}

	text, err := PDFToText(pdfPath)
	if err != nil {
		return ExtractResult{Text: text}, err
	}
	if opts.NoOCR || !isScanned(text) {
		return ExtractResult{Text: text}, nil
	}

	ocrText, err := OCRPDF(pdfPath, maxPages, logf)
	if err != nil {
		logf("warn: OCR unavailable for %s: %v", pdfPath, err)
		return ExtractResult{Text: text}, nil
	}
	return ExtractResult{Text: ocrText, OCRUsed: true}, nil
}

// extractViaXberg extracts pdfPath's text via the xberg service. It returns the
// result and ok=true only when xberg produced non-empty text; otherwise ok is
// false and the caller should fall back to the local pdftotext (+OCR) path.
//
// Non-PDF documents are sent to xberg directly (one request) without any local
// pdftotext/pdftoppm involvement. For PDFs, pdftotext is run purely as a
// text-layer detector: text-layer PDFs are sent to xberg as PDFs, while scanned
// PDFs are rendered to PNGs and sent as images.
func extractViaXberg(pdfPath string, cfg *XbergConfig, maxPages int, logf func(string, ...interface{})) (ExtractResult, bool) {
	if !isPDFPath(pdfPath) {
		return extractNonPDFViaXberg(pdfPath, cfg, logf)
	}

	detectorText, _ := PDFToText(pdfPath)

	if !isScanned(detectorText) {
		// Text-layer PDF: send the PDF bytes directly to xberg.
		data, err := os.ReadFile(pdfPath)
		if err != nil {
			logf("warn: xberg extraction failed for %s: %v; falling back to pdftotext", pdfPath, err)
			return ExtractResult{}, false
		}
		text, err := XbergExtractText(context.Background(), data, filepath.Base(pdfPath), *cfg)
		if err != nil {
			logf("warn: xberg extraction failed for %s: %v; falling back to pdftotext", pdfPath, err)
			return ExtractResult{}, false
		}
		if strings.TrimSpace(text) == "" {
			logf("warn: xberg returned no text for %s; falling back to pdftotext", pdfPath)
			return ExtractResult{}, false
		}
		return ExtractResult{Text: text, XbergUsed: true}, true
	}

	// Scanned PDF: render pages to PNG and send them as images.
	pages, cleanup, err := RenderPagePNGs(pdfPath, maxPages)
	if err != nil {
		logf("warn: xberg extraction failed for %s: %v; falling back to local OCR", pdfPath, err)
		return ExtractResult{}, false
	}
	defer cleanup()

	files := make([]XbergFile, 0, len(pages))
	for i, p := range pages {
		data, err := os.ReadFile(p)
		if err != nil {
			logf("warn: read rendered page %s: %v", p, err)
			continue
		}
		files = append(files, XbergFile{Content: data, Filename: fmt.Sprintf("page-%d.png", i+1)})
	}

	contents, err := XbergExtractFiles(context.Background(), files, *cfg)
	if err != nil {
		logf("warn: xberg extraction failed for %s: %v; falling back to local OCR", pdfPath, err)
		return ExtractResult{}, false
	}

	var parts []string
	for _, c := range contents {
		if strings.TrimSpace(c) != "" {
			parts = append(parts, c)
		}
	}
	if len(parts) == 0 {
		logf("warn: xberg returned no text for %s; falling back to local OCR", pdfPath)
		return ExtractResult{}, false
	}
	return ExtractResult{Text: strings.Join(parts, "\n\n"), XbergUsed: true}, true
}

// extractNonPDFViaXberg sends a non-PDF document (e.g. .docx) to xberg in a
// single request. It never invokes pdftotext/pdftoppm/tesseract, none of which
// can read non-PDFs: on error or empty result it logs a warning and returns
// ok=false so the caller yields an empty document.
func extractNonPDFViaXberg(path string, cfg *XbergConfig, logf func(string, ...interface{})) (ExtractResult, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		logf("warn: xberg extraction failed for %s: %v", path, err)
		return ExtractResult{}, false
	}
	contents, err := XbergExtractFiles(context.Background(), []XbergFile{{Content: data, Filename: filepath.Base(path)}}, *cfg)
	if err != nil {
		logf("warn: xberg extraction failed for %s: %v", path, err)
		return ExtractResult{}, false
	}
	text := contents[0]
	if strings.TrimSpace(text) == "" {
		logf("warn: xberg returned no text for %s", path)
		return ExtractResult{}, false
	}
	return ExtractResult{Text: text, XbergUsed: true}, true
}

// isPDFPath reports whether path has a .pdf extension (case-insensitive).
func isPDFPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

// RenderPagePNGs renders pdfPath to 300-dpi PNGs (one per page, up to maxPages)
// with pdftoppm, returning the sorted page PNG paths and a cleanup func that
// removes the temp directory. It returns an error if pdftoppm is missing or
// rendering fails.
func RenderPagePNGs(pdfPath string, maxPages int) (pages []string, cleanup func(), err error) {
	pdftoppm, err := exec.LookPath("pdftoppm")
	if err != nil {
		return nil, func() {}, fmt.Errorf("pdftoppm not found: %w (install poppler-utils)", err)
	}

	dir, err := os.MkdirTemp("", "loveval-render-*")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	prefix := filepath.Join(dir, "page")

	render := exec.Command(pdftoppm, "-r", "300", "-png", "-f", "1", "-l", strconv.Itoa(maxPages), pdfPath, prefix)
	var renderErr bytes.Buffer
	render.Stderr = &renderErr
	if err := render.Run(); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("pdftoppm: %v: %s", err, strings.TrimSpace(renderErr.String()))
	}

	pages, _ = filepath.Glob(prefix + "-*.png")
	sort.Strings(pages)
	return pages, cleanup, nil
}

// OCRPDF renders pdfPath to PNGs with pdftoppm (300 dpi) and OCRs each page with
// tesseract (Norwegian). It returns an error if the tools are missing or
// rendering fails; per-page OCR failures are skipped and logged.
func OCRPDF(pdfPath string, maxPages int, logf func(string, ...interface{})) (string, error) {
	tesseract, err := exec.LookPath("tesseract")
	if err != nil {
		return "", fmt.Errorf("tesseract not found: %w", err)
	}

	pages, cleanup, err := RenderPagePNGs(pdfPath, maxPages)
	if err != nil {
		return "", err
	}
	defer cleanup()

	var out strings.Builder
	for i, png := range pages {
		ocr := exec.Command(tesseract, png, "stdout", "-l", "nor", "--psm", "3")
		var buf, errBuf bytes.Buffer
		ocr.Stdout = &buf
		ocr.Stderr = &errBuf
		if err := ocr.Run(); err != nil {
			logf("warn: tesseract %s: %v: %s", png, err, strings.TrimSpace(errBuf.String()))
			continue
		}
		if i > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(strings.TrimSpace(buf.String()))
	}
	return out.String(), nil
}
