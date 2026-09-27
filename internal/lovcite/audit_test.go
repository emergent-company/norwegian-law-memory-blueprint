package lovcite

import "testing"

func TestAuditDeterministic(t *testing.T) {
	idx := buildStandard(t, false)
	dir := writeSeed(t, standardObjects(), standardRelationships())

	summary, err := idx.Audit(dir, 10)
	if err != nil {
		t.Fatal(err)
	}

	if summary.ScannedDocs != 2 {
		t.Errorf("ScannedDocs = %d, want 2", summary.ScannedDocs)
	}
	if summary.ActLovFound != 1 || summary.ActLovResolved != 1 {
		t.Errorf("act lov = found %d resolved %d, want 1/1", summary.ActLovFound, summary.ActLovResolved)
	}
	if summary.ActForskriftFound != 0 {
		t.Errorf("ActForskriftFound = %d, want 0", summary.ActForskriftFound)
	}
	// § 1-2 and § 2 resolve; § 9-9 does not.
	if summary.SectionFound != 3 || summary.SectionResolved != 2 {
		t.Errorf("section = found %d resolved %d, want 3/2", summary.SectionFound, summary.SectionResolved)
	}
	if summary.TotalFound != 4 || summary.TotalResolved != 3 || summary.TotalUnresolved != 1 {
		t.Errorf("totals = found %d resolved %d unresolved %d, want 4/3/1",
			summary.TotalFound, summary.TotalResolved, summary.TotalUnresolved)
	}
	if summary.ResolutionRate != 0.75 {
		t.Errorf("ResolutionRate = %v, want 0.75", summary.ResolutionRate)
	}
	if len(summary.Samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(summary.Samples))
	}
	sm := summary.Samples[0]
	if sm.Ref != "§ 9-9" || sm.Key != "lov/2005-06-17-62" {
		t.Errorf("sample = %+v", sm)
	}
	if sm.File != "Law.jsonl" {
		t.Errorf("sample file = %q", sm.File)
	}
}

func TestAuditIsRepeatable(t *testing.T) {
	idx := buildStandard(t, false)
	dir := writeSeed(t, standardObjects(), standardRelationships())

	a, err := idx.Audit(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	b, err := idx.Audit(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if a.TotalFound != b.TotalFound || a.TotalResolved != b.TotalResolved {
		t.Errorf("audit not repeatable: %+v vs %+v", a, b)
	}
	for i := range a.Samples {
		if a.Samples[i] != b.Samples[i] {
			t.Errorf("sample %d differs", i)
		}
	}
}

func TestSampleUnresolvedEvenSpread(t *testing.T) {
	all := make([]UnresolvedSample, 10)
	for i := range all {
		all[i] = UnresolvedSample{Ref: string(rune('a' + i))}
	}
	got := sampleUnresolved(all, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Evenly spaced: indices 0, 3, 6.
	if got[0].Ref != "a" || got[1].Ref != "d" || got[2].Ref != "g" {
		t.Errorf("spread = %+v", got)
	}
}

func TestSampleUnresolvedFewerThanN(t *testing.T) {
	all := []UnresolvedSample{{Ref: "x"}, {Ref: "y"}}
	got := sampleUnresolved(all, 5)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}
