package evaldata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestXbergExtractTextPDFNoConfig(t *testing.T) {
	var gotFilename, gotCT string
	var hasConfig bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Errorf("path = %q, want /extract", r.URL.Path)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		files := r.MultipartForm.File["files"]
		if len(files) != 1 {
			t.Errorf("files count = %d, want 1", len(files))
			http.Error(w, "bad files", http.StatusBadRequest)
			return
		}
		gotFilename = files[0].Filename
		gotCT = files[0].Header.Get("Content-Type")
		_, hasConfig = r.MultipartForm.Value["config"]
		writeXbergJSON(w, http.StatusOK, `{"results":[{"content":"hello world","metadata":{},"tables":[],"images":[]}],"errors":[],"summary":{}}`)
	}))
	defer srv.Close()

	got, err := XbergExtractText(context.Background(), []byte("%PDF-1.4 fake"), "exam.pdf", XbergConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("XbergExtractText: %v", err)
	}
	if got != "hello world" {
		t.Errorf("content = %q, want %q", got, "hello world")
	}
	if gotFilename != "exam.pdf" {
		t.Errorf("filename = %q, want %q", gotFilename, "exam.pdf")
	}
	if gotCT != "application/pdf" {
		t.Errorf("content-type = %q, want %q", gotCT, "application/pdf")
	}
	if hasConfig {
		t.Error("config field should be absent when ForceOCR=false and no lang/backend")
	}
}

func TestXbergExtractTextConfigField(t *testing.T) {
	var gotConfig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		gotConfig = r.FormValue("config")
		writeXbergJSON(w, http.StatusOK, `{"results":[{"content":"ok"}],"errors":[],"summary":{}}`)
	}))
	defer srv.Close()

	_, err := XbergExtractText(context.Background(), []byte("data"), "exam.docx", XbergConfig{
		BaseURL:     srv.URL,
		ForceOCR:    true,
		OCRLanguage: "nor",
		OCRBackend:  "tesseract",
	})
	if err != nil {
		t.Fatalf("XbergExtractText: %v", err)
	}

	var cfg struct {
		ForceOCR bool `json:"force_ocr"`
		OCR      struct {
			Backend  string   `json:"backend"`
			Language []string `json:"language"`
		} `json:"ocr"`
	}
	if err := json.Unmarshal([]byte(gotConfig), &cfg); err != nil {
		t.Fatalf("config field is not valid JSON %q: %v", gotConfig, err)
	}
	if !cfg.ForceOCR {
		t.Errorf("force_ocr = %v, want true", cfg.ForceOCR)
	}
	if cfg.OCR.Backend != "tesseract" {
		t.Errorf("ocr.backend = %q, want tesseract", cfg.OCR.Backend)
	}
	if len(cfg.OCR.Language) != 1 || cfg.OCR.Language[0] != "nor" {
		t.Errorf("ocr.language = %v, want [nor]", cfg.OCR.Language)
	}
}

func TestXbergExtractTextEmptyResultsWithErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeXbergJSON(w, http.StatusOK, `{"results":[],"errors":[{"message":"parse failed","error":"","file":"exam.pdf"}],"summary":{}}`)
	}))
	defer srv.Close()

	_, err := XbergExtractText(context.Background(), []byte("x"), "exam.pdf", XbergConfig{BaseURL: srv.URL})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "file: parse failed") {
		t.Errorf("error = %q, want it to contain %q", err, "file: parse failed")
	}
}

func TestXbergExtractTextNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeXbergJSON(w, http.StatusInternalServerError, `{"error":"boom","message":"internal","detail":"nope"}`)
	}))
	defer srv.Close()

	_, err := XbergExtractText(context.Background(), []byte("x"), "exam.pdf", XbergConfig{BaseURL: srv.URL})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want it to mention status 500", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want it to contain %q", err, "boom")
	}
}

func TestXbergExtractTextFallsBackToPdfToText(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not in PATH")
	}

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "exam.pdf")
	if err := os.WriteFile(pdfPath, minimalPDF(), 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	res, err := ExtractText(pdfPath, ExtractOptions{
		Converter: "xberg",
		Xberg:     &XbergConfig{BaseURL: srv.URL},
	})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if res.XbergUsed {
		t.Error("XbergUsed = true, want false (should fall back to pdftotext)")
	}
	if !strings.Contains(res.Text, "Hello World") {
		t.Errorf("text = %q, want it to contain %q", res.Text, "Hello World")
	}
}

func TestExtractViaXbergNonPDF(t *testing.T) {
	var gotFilename, gotCT string
	var gotRequest bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		if r.URL.Path != "/extract" {
			t.Errorf("path = %q, want /extract", r.URL.Path)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		files := r.MultipartForm.File["files"]
		if len(files) != 1 {
			t.Errorf("files count = %d, want 1", len(files))
			http.Error(w, "bad files", http.StatusBadRequest)
			return
		}
		gotFilename = files[0].Filename
		gotCT = files[0].Header.Get("Content-Type")
		writeXbergJSON(w, http.StatusOK, `{"results":[{"content":"DOCX TEXT"}],"errors":[],"summary":{}}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.docx")
	if err := os.WriteFile(path, []byte("PK fake docx bytes"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}

	res, err := ExtractText(path, ExtractOptions{
		Converter: "xberg",
		Xberg:     &XbergConfig{BaseURL: srv.URL},
	})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !gotRequest {
		t.Fatal("server did not receive a request")
	}
	if !res.XbergUsed {
		t.Error("XbergUsed = false, want true")
	}
	if res.Text != "DOCX TEXT" {
		t.Errorf("Text = %q, want %q", res.Text, "DOCX TEXT")
	}
	if gotFilename != "sample.docx" {
		t.Errorf("filename = %q, want %q", gotFilename, "sample.docx")
	}
	if gotCT != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("content-type = %q, want docx MIME", gotCT)
	}
}

func TestSplitLanguages(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"nor", []string{"nor"}},
		{"nor+eng", []string{"nor", "eng"}},
		{"", nil},
		{"+nor+", []string{"nor"}},
	}
	for _, c := range cases {
		got := splitLanguages(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitLanguages(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitLanguages(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestMimeForFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a.pdf", "application/pdf"},
		{"B.PDF", "application/pdf"},
		{"a.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"a.png", "image/png"},
		{"a.jpg", "image/jpeg"},
		{"a.jpeg", "image/jpeg"},
		{"a.tif", "image/tiff"},
		{"a.tiff", "image/tiff"},
		{"a.gif", "application/octet-stream"},
	}
	for _, c := range cases {
		if got := mimeForFilename(c.in); got != c.want {
			t.Errorf("mimeForFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestXbergExtractFilesMultiOrder(t *testing.T) {
	type part struct {
		filename string
		ctype    string
		content  string
	}
	var got []part
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Errorf("path = %q, want /extract", r.URL.Path)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		files := r.MultipartForm.File["files"]
		for _, f := range files {
			got = append(got, part{
				filename: f.Filename,
				ctype:    f.Header.Get("Content-Type"),
			})
		}
		writeXbergJSON(w, http.StatusOK, `{"results":[{"content":"first"},{"content":"second"}],"errors":[],"summary":{}}`)
	}))
	defer srv.Close()

	files := []XbergFile{
		{Content: []byte("png-bytes"), Filename: "page-1.png"},
		{Content: []byte("png-bytes-2"), Filename: "page-2.png"},
	}
	contents, err := XbergExtractFiles(context.Background(), files, XbergConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("XbergExtractFiles: %v", err)
	}
	if len(contents) != 2 || contents[0] != "first" || contents[1] != "second" {
		t.Errorf("contents = %v, want [first second]", contents)
	}
	if len(got) != 2 {
		t.Fatalf("server saw %d files, want 2", len(got))
	}
	if got[0].filename != "page-1.png" || got[1].filename != "page-2.png" {
		t.Errorf("filenames = %q, %q; want page-1.png, page-2.png", got[0].filename, got[1].filename)
	}
	if got[0].ctype != "image/png" || got[1].ctype != "image/png" {
		t.Errorf("content-types = %q, %q; want image/png, image/png", got[0].ctype, got[1].ctype)
	}
}

func TestXbergExtractTextWrapsFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeXbergJSON(w, http.StatusOK, `{"results":[{"content":"wrapped"}],"errors":[],"summary":{}}`)
	}))
	defer srv.Close()

	got, err := XbergExtractText(context.Background(), []byte("x"), "exam.pdf", XbergConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("XbergExtractText: %v", err)
	}
	if got != "wrapped" {
		t.Errorf("content = %q, want %q", got, "wrapped")
	}
}

func writeXbergJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

// minimalPDF returns a well-formed single-page PDF containing the text
// "Hello World", with a correct xref table so pdftotext can extract it.
func minimalPDF() []byte {
	streamContent := "BT /F1 24 Tf 72 720 Td (Hello World) Tj ET\n"

	var b bytes.Buffer
	var offsets []int
	writeObj := func(body string) {
		offsets = append(offsets, b.Len())
		b.WriteString(body)
	}

	writeObj("%PDF-1.4\n")
	writeObj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	writeObj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	writeObj("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	writeObj(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(streamContent), streamContent))
	writeObj("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	xrefStart := b.Len()
	fmt.Fprintf(&b, "xref\n0 6\n")
	fmt.Fprintf(&b, "0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return b.Bytes()
}
