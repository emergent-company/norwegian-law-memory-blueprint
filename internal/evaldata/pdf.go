package evaldata

import (
	"bytes"
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

// ExtractOptions controls text extraction, including the OCR fallback.
type ExtractOptions struct {
	OCRMaxPages int                          // max pages to OCR (0 -> DefaultOCRMaxPages)
	NoOCR       bool                         // disable the OCR fallback entirely
	Logf        func(string, ...interface{}) // optional diagnostics sink
}

// ExtractResult is the outcome of ExtractText.
type ExtractResult struct {
	Text    string // extracted text (pdftotext output, or OCR when used)
	OCRUsed bool   // whether the OCR fallback produced the text
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
func ExtractText(pdfPath string, opts ExtractOptions) (ExtractResult, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}

	text, err := PDFToText(pdfPath)
	if err != nil {
		return ExtractResult{Text: text}, err
	}
	if opts.NoOCR || !isScanned(text) {
		return ExtractResult{Text: text}, nil
	}

	maxPages := opts.OCRMaxPages
	if maxPages <= 0 {
		maxPages = DefaultOCRMaxPages
	}
	ocrText, err := OCRPDF(pdfPath, maxPages, logf)
	if err != nil {
		logf("warn: OCR unavailable for %s: %v", pdfPath, err)
		return ExtractResult{Text: text}, nil
	}
	return ExtractResult{Text: ocrText, OCRUsed: true}, nil
}

// OCRPDF renders pdfPath to PNGs with pdftoppm (300 dpi) and OCRs each page with
// tesseract (Norwegian). It returns an error if the tools are missing or
// rendering fails; per-page OCR failures are skipped and logged.
func OCRPDF(pdfPath string, maxPages int, logf func(string, ...interface{})) (string, error) {
	pdftoppm, err := exec.LookPath("pdftoppm")
	if err != nil {
		return "", fmt.Errorf("pdftoppm not found: %w (install poppler-utils)", err)
	}
	tesseract, err := exec.LookPath("tesseract")
	if err != nil {
		return "", fmt.Errorf("tesseract not found: %w", err)
	}

	dir, err := os.MkdirTemp("", "loveval-ocr-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	prefix := filepath.Join(dir, "page")

	render := exec.Command(pdftoppm, "-r", "300", "-png", "-f", "1", "-l", strconv.Itoa(maxPages), pdfPath, prefix)
	var renderErr bytes.Buffer
	render.Stderr = &renderErr
	if err := render.Run(); err != nil {
		return "", fmt.Errorf("pdftoppm: %v: %s", err, strings.TrimSpace(renderErr.String()))
	}

	pages, _ := filepath.Glob(prefix + "-*.png")
	sort.Strings(pages)

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
