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

func TestJSONEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b any
		want bool
	}{
		{"equal strings", "x", "x", true},
		{"differing strings", "x", "y", false},
		{"equal numbers across int/float", int(1), float64(1), true},
		{"differing numbers", float64(1), float64(2), false},
		{"equal nested objects", map[string]any{"a": float64(1), "b": []any{float64(1), float64(2)}}, map[string]any{"b": []any{float64(1), float64(2)}, "a": float64(1)}, true},
		{"differing nested objects", map[string]any{"a": float64(1)}, map[string]any{"a": float64(2)}, false},
		{"missing key", nil, float64(1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsonEqual(tc.a, tc.b); got != tc.want {
				t.Fatalf("jsonEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestBuildSyncPropsDelta(t *testing.T) {
	cases := []struct {
		name string
		seed seedObjectLine
		live map[string]any
		want map[string]any // nil means no patch
	}{
		{
			name: "non-date changed value yields patch",
			seed: seedObjectLine{Type: "Law", Key: "k1", Properties: map[string]any{"title": "New Title"}},
			live: map[string]any{"title": "Old Title"},
			want: map[string]any{"title": "New Title"},
		},
		{
			name: "non-date equal nested value yields no patch",
			seed: seedObjectLine{Type: "Foo", Key: "k2", Properties: map[string]any{"meta": map[string]any{"a": float64(1), "b": []any{float64(1), float64(2)}}}},
			live: map[string]any{"meta": map[string]any{"b": []any{float64(1), float64(2)}, "a": float64(1)}},
			want: nil,
		},
		{
			name: "date already canonical yields no patch",
			seed: seedObjectLine{Type: "Law", Key: "k3", Properties: map[string]any{"date_in_force": "2006-01-01"}},
			live: map[string]any{"date_in_force": "2006-01-01T00:00:00Z"},
			want: nil,
		},
		{
			name: "missing key yields patch",
			seed: seedObjectLine{Type: "Law", Key: "k4", Properties: map[string]any{"title": "X"}},
			live: map[string]any{},
			want: map[string]any{"title": "X"},
		},
		{
			name: "non-canonical bare date yields patch",
			seed: seedObjectLine{Type: "Law", Key: "k5", Properties: map[string]any{"date_in_force": "2006-01-01"}},
			live: map[string]any{"date_in_force": "2006-01-01"},
			want: map[string]any{"date_in_force": "2006-01-01"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSyncPropsDelta(tc.seed, tc.live)
			if !jsonEqual(got, tc.want) {
				t.Fatalf("buildSyncPropsDelta(%v) = %v, want %v", tc.seed, got, tc.want)
			}
		})
	}
}
