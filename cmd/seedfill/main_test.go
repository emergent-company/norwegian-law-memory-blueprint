package main

import "testing"

func TestIsDateValue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"bare date", "2006-01-02", true},
		{"rfc3339 utc", "2004-03-26T00:00:00Z", true},
		{"rfc3339 offset", "2004-03-26T00:00:00+01:00", true},
		{"empty string", "", false},
		{"not a date", "not-a-date", false},
		{"invalid month", "2004-13-01", false},
		{"int", 20040326, false},
		{"float", 20040326.0, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDateValue(tc.in); got != tc.want {
				t.Fatalf("isDateValue(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
