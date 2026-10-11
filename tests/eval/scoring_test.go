package eval

import (
	"math"
	"testing"
)

// citationWant is the expected ScoreCitations output for a single table case.
// hallucinatedRef, when non-empty, asserts the exact HallucinatedRefs slice.
type citationWant struct {
	precision, recall, f1, resolutionRate float64
	totalCited, resolved, hallucinated    int
	hallucinatedRef                       string
}

func TestScoreCitations(t *testing.T) {
	idx, abbrevs, err := buildOffline()
	if err != nil {
		t.Fatalf("buildOffline: %v", err)
	}

	cases := []struct {
		name   string
		answer string
		gold   []string
		want   citationWant
	}{
		{
			name:   "exact match",
			answer: "aml § 15-3",
			gold:   []string{"lov/2005-06-17-62#§15-3"},
			want:   citationWant{precision: 1, recall: 1, f1: 1, resolutionRate: 1, totalCited: 1, resolved: 1, hallucinated: 0},
		},
		{
			name:   "partial same act different section",
			answer: "aml § 15-4",
			gold:   []string{"lov/2005-06-17-62#§15-3"},
			want:   citationWant{precision: 0.5, recall: 0.5, f1: 0.5, resolutionRate: 1, totalCited: 1, resolved: 1, hallucinated: 0},
		},
		{
			name:   "wrong act that resolves",
			answer: "fkjl § 16",
			gold:   []string{"lov/2005-06-17-62#§15-3"},
			want:   citationWant{precision: 0, recall: 0, f1: 0, resolutionRate: 1, totalCited: 1, resolved: 1, hallucinated: 0},
		},
		{
			// A section that fails to resolve is still canonicalised to the
			// "same act" form, so it earns 0.5 partial credit toward
			// precision/recall even though it is hallucinated and drives
			// ResolutionRate to 0.
			name:   "hallucinated section",
			answer: "aml § 99-99",
			gold:   []string{"lov/2005-06-17-62#§15-3"},
			want:   citationWant{precision: 0.5, recall: 0.5, f1: 0.5, resolutionRate: 0, totalCited: 1, resolved: 0, hallucinated: 1, hallucinatedRef: "lov/2005-06-17-62#§99-99"},
		},
		{
			name:   "empty answer",
			answer: "",
			gold:   []string{"lov/2005-06-17-62#§15-3"},
			want:   citationWant{precision: 0, recall: 0, f1: 0, resolutionRate: 1, totalCited: 0, resolved: 0, hallucinated: 0},
		},
		{
			name:   "empty gold",
			answer: "aml § 15-3",
			gold:   nil,
			want:   citationWant{precision: 0, recall: 1, f1: 0, resolutionRate: 1, totalCited: 1, resolved: 1, hallucinated: 0},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := ScoreCitations(c.answer, c.gold, idx, abbrevs)
			assertFloat(t, "precision", m.Precision, c.want.precision)
			assertFloat(t, "recall", m.Recall, c.want.recall)
			assertFloat(t, "f1", m.F1, c.want.f1)
			assertFloat(t, "resolution-rate", m.ResolutionRate, c.want.resolutionRate)
			if m.TotalCited != c.want.totalCited {
				t.Errorf("TotalCited = %d, want %d", m.TotalCited, c.want.totalCited)
			}
			if m.Resolved != c.want.resolved {
				t.Errorf("Resolved = %d, want %d", m.Resolved, c.want.resolved)
			}
			if m.Hallucinated != c.want.hallucinated {
				t.Errorf("Hallucinated = %d, want %d", m.Hallucinated, c.want.hallucinated)
			}
			if c.want.hallucinatedRef != "" {
				if len(m.HallucinatedRefs) != 1 || m.HallucinatedRefs[0] != c.want.hallucinatedRef {
					t.Errorf("HallucinatedRefs = %v, want [%s]", m.HallucinatedRefs, c.want.hallucinatedRef)
				}
			} else if len(m.HallucinatedRefs) != 0 {
				t.Errorf("HallucinatedRefs = %v, want empty", m.HallucinatedRefs)
			}
		})
	}
}

// assertFloat asserts that got == want within a tight tolerance.
func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
