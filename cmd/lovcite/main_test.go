package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// smokeSeed writes a minimal seed corpus to a temp dir and returns its path.
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
		`{"type":"Law","key":"lov/2005-06-17-62","properties":{"ref_id":"lov/2005-06-17-62","content":"### § 1-1 — Formål\n\nFormålsteksten her.\n\n### § 1-2 — Omfang\n\nSe § 9-9.\n"}}`+"\n")
	write(objDir, "LegalParagraph.jsonl",
		`{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"law_ref_id":"lov/2005-06-17-62","paragraph_num":"§ 1-1","section_id":"kapittel-1-paragraf-1","content":"Formålsteksten her."}}`+"\n"+
			`{"type":"LegalParagraph","key":"lov/2005-06-17-62#kapittel-1-paragraf-2","properties":{"law_ref_id":"lov/2005-06-17-62","paragraph_num":"§ 1-2","section_id":"kapittel-1-paragraf-2","content":"Omfang."}}`+"\n")
	write(relDir, "HAS_PARAGRAPH.jsonl",
		`{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-1","properties":{"position":1}}`+"\n"+
			`{"type":"HAS_PARAGRAPH","srcKey":"lov/2005-06-17-62","dstKey":"lov/2005-06-17-62#kapittel-1-paragraf-2","properties":{"position":2}}`+"\n")
	return dir
}

// capture runs fn with stdout/stderr redirected to buffers.
func capture(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	oldOut, oldErr := out, errOut
	var buf, errBuf bytes.Buffer
	out, errOut = &buf, &errBuf
	defer func() { out, errOut = oldOut, oldErr }()

	code := fn()
	return code, buf.String() + errBuf.String()
}

func TestSmokeAudit(t *testing.T) {
	dir := smokeSeed(t)
	code, output := capture(t, func() int {
		return runAudit([]string{"--seed", dir, "--samples", "5"})
	})
	if code != 0 {
		t.Fatalf("audit exit = %d, want 0\n%s", code, output)
	}
	if !strings.Contains(output, "scanned docs:            1") {
		t.Errorf("audit output missing scanned docs:\n%s", output)
	}
	// § 1-2 in body is resolved (not a heading); § 9-9 is unresolved.
	if !strings.Contains(output, "total refs:") {
		t.Errorf("audit output missing totals:\n%s", output)
	}
}

func TestSmokeValidateRef(t *testing.T) {
	dir := smokeSeed(t)
	code, output := capture(t, func() int {
		return runValidateRef([]string{"--seed", dir, "lov/2005-06-17-62#§1-1"})
	})
	if code != 0 {
		t.Fatalf("validate-ref exit = %d, want 0\n%s", code, output)
	}
	if !strings.Contains(output, "kapittel-1-paragraf-1") {
		t.Errorf("validate-ref output:\n%s", output)
	}
}

func TestSmokeValidateRefMissing(t *testing.T) {
	dir := smokeSeed(t)
	code, _ := capture(t, func() int {
		return runValidateRef([]string{"--seed", dir, "lov/2005-06-17-62#§9-9"})
	})
	if code != 1 {
		t.Fatalf("validate-ref exit = %d, want 1 for unresolved ref", code)
	}
}

func TestSmokeVerifyQuoteMatch(t *testing.T) {
	dir := smokeSeed(t)
	code, output := capture(t, func() int {
		return runVerifyQuote([]string{"--seed", dir, "--ref", "lov/2005-06-17-62#§1-1", "--quote", "Formålsteksten"})
	})
	if code != 0 {
		t.Fatalf("verify-quote exit = %d, want 0\n%s", code, output)
	}
	if !strings.Contains(output, "MATCH") {
		t.Errorf("verify-quote output:\n%s", output)
	}
}

func TestSmokeVerifyQuoteMismatch(t *testing.T) {
	dir := smokeSeed(t)
	code, output := capture(t, func() int {
		return runVerifyQuote([]string{"--seed", dir, "--ref", "lov/2005-06-17-62#§1-1", "--quote", "ikke til stede"})
	})
	if code != 1 {
		t.Fatalf("verify-quote exit = %d, want 1\n%s", code, output)
	}
	if !strings.Contains(output, "NO MATCH") {
		t.Errorf("verify-quote output:\n%s", output)
	}
}
