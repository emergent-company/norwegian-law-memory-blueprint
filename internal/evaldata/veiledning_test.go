package evaldata

import (
	"reflect"
	"testing"
)

func TestSplitPointsParagraphs(t *testing.T) {
	text := "Første punkt.\n\nAndre punkt med\nlinjeskift.\n\nTredje."
	got := SplitPoints(text)
	want := []string{"Første punkt.", "Andre punkt med linjeskift.", "Tredje."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitPointsNumberedBullets(t *testing.T) {
	text := "1. Første punkt.\n2. Andre punkt.\n3) Tredje punkt."
	got := SplitPoints(text)
	want := []string{"1. Første punkt.", "2. Andre punkt.", "3) Tredje punkt."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSplitPointsDropsNoise(t *testing.T) {
	text := "Sensorveiledning JUS1111\n\nFaktisk innhold her.\n\nSide 2 av 4\n\n3"
	got := SplitPoints(text)
	if len(got) != 1 || got[0] != "Faktisk innhold her." {
		t.Fatalf("got %q", got)
	}
}

func TestSplitPointsEmpty(t *testing.T) {
	if got := SplitPoints(""); len(got) != 0 {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestIsBulletLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"1. Punkt", true},
		{"1) Punkt", true},
		{"a) Punkt", true},
		{"1.1 Punkt", true},
		{"§ 1 Første", true},
		{"§ 1.2 Andre", true},
		{"Vanlig tekst", false},
		{"", false},
		{"§ Uten nummer", false},
	}
	for _, c := range cases {
		if got := isBulletLine(c.line); got != c.want {
			t.Errorf("isBulletLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
