package lovcite

import "testing"

func TestNormalizeParagraphNum(t *testing.T) {
	cases := []struct{ in, want string }{
		{"§ 1-3", "1-3"},
		{"§§ 6", "6"},
		{"§ 1-1 a", "1-1 a"},
		{"§1-1a", "1-1a"},
		{"§ 10-1", "10-1"},
		{"  § 4  ", "4"},
		{"§", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeParagraphNum(c.in); got != c.want {
			t.Errorf("NormalizeParagraphNum(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\nb   c", "a b c"},
		{"  leading and trailing  ", "leading and trailing"},
		{"tab\tand\nnewline", "tab and newline"},
		{"", ""},
		{"single", "single"},
	}
	for _, c := range cases {
		if got := NormalizeWhitespace(c.in); got != c.want {
			t.Errorf("NormalizeWhitespace(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeSectionToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1-3", "1-3"},
		{"1 - 3", "1-3"},
		{"1–3", "1-3"}, // en dash
		{"1—3", "1-3"}, // em dash
		{"1-3a", "1-3a"},
		{"22", "22"},
	}
	for _, c := range cases {
		if got := normalizeSectionToken(c.in); got != c.want {
			t.Errorf("normalizeSectionToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
