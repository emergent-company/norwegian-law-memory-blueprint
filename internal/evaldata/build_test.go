package evaldata

import "testing"

func TestPickOppgavePrefersBokmal(t *testing.T) {
	nb := ManifestEntry{URL: "nb.pdf", Language: "nb"}
	nn := ManifestEntry{URL: "nn.pdf", Language: "nn"}
	got := pickOppgave(map[string]ManifestEntry{"nb": nb, "nn": nn})
	if got == nil || got.URL != "nb.pdf" {
		t.Fatalf("got %+v, want nb", got)
	}
	// Only nynorsk available.
	got = pickOppgave(map[string]ManifestEntry{"nn": nn})
	if got == nil || got.URL != "nn.pdf" {
		t.Fatalf("got %+v, want nn", got)
	}
	if pickOppgave(nil) != nil {
		t.Fatal("expected nil for empty")
	}
}

func TestPickVeiledningMatchesLanguage(t *testing.T) {
	nb := ManifestEntry{URL: "veil-nb.pdf", Language: "nb"}
	nn := ManifestEntry{URL: "veil-nn.pdf", Language: "nn"}
	// Prefer same language as the paper (nn).
	got := pickVeiledning(map[string]ManifestEntry{"nb": nb, "nn": nn}, "nn")
	if got == nil || got.URL != "veil-nn.pdf" {
		t.Fatalf("got %+v, want nn", got)
	}
	// Requested language absent -> any.
	got = pickVeiledning(map[string]ManifestEntry{"nb": nb}, "nn")
	if got == nil || got.URL != "veil-nb.pdf" {
		t.Fatalf("got %+v, want nb fallback", got)
	}
}

func TestManifestHasURL(t *testing.T) {
	m := &Manifest{Entries: []ManifestEntry{
		{URL: "https://uio/a.pdf", SHA256: "abc"},
	}}
	if !m.HasURL("https://uio/a.pdf", "abc") {
		t.Error("expected match")
	}
	if m.HasURL("https://uio/a.pdf", "different") {
		t.Error("hash mismatch must not match")
	}
	if m.HasURL("https://uio/b.pdf", "abc") {
		t.Error("url mismatch must not match")
	}
}

func TestRelCachePathSanitizes(t *testing.T) {
	got := RelCachePath("JUS1111", "v26", "a/b.pdf")
	if got != "JUS1111/v26/a_b.pdf" {
		t.Fatalf("got %q", got)
	}
	got = RelCachePath("JUS1111", "", "x.pdf")
	if got != "JUS1111/_/x.pdf" {
		t.Fatalf("got %q", got)
	}
}
