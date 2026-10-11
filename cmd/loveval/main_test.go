package main

import "testing"

func TestUnresolvedSidecar(t *testing.T) {
	got := unresolvedSidecar("evals/dataset/uio.jsonl")
	if got != "evals/dataset/uio.unresolved.json" {
		t.Fatalf("got %q", got)
	}
	got = unresolvedSidecar("uio.jsonl")
	if got != "uio.unresolved.json" {
		t.Fatalf("got %q", got)
	}
}

func TestRunBuildUnsupportedSource(t *testing.T) {
	// Unsupported source must fail fast (exit 2) without touching the network.
	if code := runBuild([]string{"--source", "nope"}); code != 2 {
		t.Fatalf("runBuild exit = %d, want 2", code)
	}
}

func TestRunFetchUnsupportedSource(t *testing.T) {
	if code := runFetch([]string{"--source", "nope"}); code != 2 {
		t.Fatalf("runFetch exit = %d, want 2", code)
	}
}
