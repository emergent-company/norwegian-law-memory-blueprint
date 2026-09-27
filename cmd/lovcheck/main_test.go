package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// smokeSeed writes a small valid seed corpus and returns its dir.
func smokeSeed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	objDir := filepath.Join(dir, "objects")
	relDir := filepath.Join(dir, "relationships")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(objDir, "Law.jsonl",
		`{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62","date_of_publication":"2005-06-17"}}`+"\n")
	write(objDir, "LegalParagraph.jsonl",
		`{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"law_ref_id":"lov/2005-06-17-62","section_id":"kapittel-1-paragraf-1","paragraph_num":"§ 1-1"}}`+"\n")
	write(relDir, "HAS_PARAGRAPH.jsonl",
		`{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"position":1}}`+"\n")
	return dir
}

// capture runs fn with stdout/stderr redirected to a buffer.
func capture(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	oldOut, oldErr := out, errOut
	var buf, errBuf bytes.Buffer
	out, errOut = &buf, &errBuf
	defer func() { out, errOut = oldOut, oldErr }()
	code := fn()
	return code, buf.String() + errBuf.String()
}

func TestSmokeCheckClean(t *testing.T) {
	dir := smokeSeed(t)
	code, output := capture(t, func() int {
		return runCheck([]string{"--seed", dir, "--samples", "5", "--schema", filepath.Join(dir, "nope.yaml")})
	})
	if code != 0 {
		t.Fatalf("check exit = %d, want 0\n%s", code, output)
	}
	if !strings.Contains(output, "total violations: 0") {
		t.Errorf("expected zero violations:\n%s", output)
	}
	if !strings.Contains(output, "V4") || !strings.Contains(output, "V8") {
		t.Errorf("missing checks in output:\n%s", output)
	}
}

func TestSmokeCheckReportsViolation(t *testing.T) {
	dir := smokeSeed(t)
	// Add a duplicate Law line.
	f, err := os.OpenFile(filepath.Join(dir, "objects", "Law.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	code, output := capture(t, func() int {
		return runCheck([]string{"--seed", dir, "--schema", filepath.Join(dir, "nope.yaml")})
	})
	if !strings.Contains(output, "total violations: 1") {
		t.Errorf("expected one violation:\n%s", output)
	}
	// Report-only by default: still exits 0.
	if code != 0 {
		t.Fatalf("check exit = %d, want 0 (report-only default)", code)
	}
}

func TestSmokeCheckMaxViolationsExitCode(t *testing.T) {
	dir := smokeSeed(t)
	// Add a duplicate so there is exactly one violation.
	f, err := os.OpenFile(filepath.Join(dir, "objects", "Law.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62"}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	code, _ := capture(t, func() int {
		return runCheck([]string{"--seed", dir, "--max-violations", "0", "--schema", filepath.Join(dir, "nope.yaml")})
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 when violations > max-violations", code)
	}
}

func TestSmokeCheckJSON(t *testing.T) {
	dir := smokeSeed(t)
	code, output := capture(t, func() int {
		return runCheck([]string{"--seed", dir, "--json", "--schema", filepath.Join(dir, "nope.yaml")})
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, output)
	}
	if !strings.Contains(output, `"total_violations"`) {
		t.Errorf("expected JSON report:\n%s", output)
	}
}
