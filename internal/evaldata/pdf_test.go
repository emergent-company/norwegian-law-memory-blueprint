package evaldata

import "testing"

func TestIsScanned(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"  \n\t ", true},
		{short("a", OCRMinChars-1), true},
		{short("a", OCRMinChars), false},
		{short("a", OCRMinChars+10), false},
	}
	for _, c := range cases {
		if got := isScanned(c.text); got != c.want {
			t.Errorf("isScanned(len=%d) = %v, want %v", len(c.text), got, c.want)
		}
	}
}

// short builds a string of n non-space runes.
func short(r string, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = []rune(r)[0]
	}
	return string(out)
}

func TestIsScannedCountsNonSpaceOnly(t *testing.T) {
	// 300 spaces + 5 letters -> only 5 non-space chars -> scanned.
	text := ""
	for i := 0; i < 300; i++ {
		text += " "
	}
	text += "abcde"
	if !isScanned(text) {
		t.Error("whitespace should not count toward the threshold")
	}
}
