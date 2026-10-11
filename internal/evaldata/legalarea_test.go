package evaldata

import "testing"

func TestLegalAreaForCourse(t *testing.T) {
	cases := map[string]string{
		"JUS1111": "privatrett",
		"JUS1211": "privatrett",
		"JUS2111": "statsforfatningsrett",
		"JUS2211": "forvaltningsrett",
		"JUS3111": "formuerett",
		"JUS3112": "formuerett",
		"JUS3211": "strafferett",
		"JUS3212": "strafferett",
		"JUS3213": "formuerett",
		"JUS3220": "rettshistorie",
		"JUS4111": "metode",
		"JUS4121": "rettsteori",
		"JUS4122": "rettsteori",
		"JUS4123": "rettsteori",
		"JUS4211": "strafferett",
		"JUR1000": "privatrett",
		"JUR4000": "privatrett",
	}
	for course, want := range cases {
		if got := LegalAreaForCourse(course); got != want {
			t.Errorf("LegalAreaForCourse(%q) = %q, want %q", course, got, want)
		}
	}
}

func TestLegalAreaForCourseUnknown(t *testing.T) {
	for _, c := range []string{"", "JUS9999", "EXPHIL03", "TYSJUR1"} {
		if got := LegalAreaForCourse(c); got != "" {
			t.Errorf("LegalAreaForCourse(%q) = %q, want empty", c, got)
		}
	}
}
