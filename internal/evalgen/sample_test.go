package evalgen

import "testing"

func TestSampleIndicesDeterministic(t *testing.T) {
	a := sampleIndices(rngFor(42), 100, 10)
	b := sampleIndices(rngFor(42), 100, 10)
	if len(a) != 10 || len(b) != 10 {
		t.Fatalf("want 10 indices, got %d and %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("not deterministic at %d: %d != %d", i, a[i], b[i])
		}
	}
}

func TestSampleIndicesDistinct(t *testing.T) {
	got := sampleIndices(rngFor(1), 10, 10)
	seen := map[int]bool{}
	for _, v := range got {
		if seen[v] {
			t.Fatalf("duplicate index %d", v)
		}
		seen[v] = true
	}
	if len(got) != 10 {
		t.Fatalf("want 10, got %d", len(got))
	}
}

func TestSampleIndicesClamp(t *testing.T) {
	if got := sampleIndices(rngFor(1), 5, 9); len(got) != 5 {
		t.Fatalf("k>n should return n indices, got %d", len(got))
	}
	if got := sampleIndices(rngFor(1), 5, 0); len(got) != 0 {
		t.Fatalf("k=0 should return empty, got %d", len(got))
	}
}
