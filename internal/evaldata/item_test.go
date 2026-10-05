package evaldata

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestItemJSONLRoundTrip(t *testing.T) {
	it := NewItem()
	it.ID = "uio-jus1111-v26"
	it.Course = "JUS1111"
	it.Semester = "v26"
	it.Question = "Hva er spørsmålet?"
	it.GoldPoints = []string{"Første punkt.", "Andre punkt."}
	it.GoldRefs = []string{"lov/2002-06-21-34#§16"}
	it.LegalArea = ""
	it.Language = "nb"
	it.SourceURL = "https://www.uio.no/studier/emner/jus/jus/JUS1111/oppgaver/index.html"
	it.OppgaveURL = "https://www.uio.no/x.pdf"
	it.VeiledningURL = "https://www.uio.no/y.pdf"

	// Marshal then unmarshal and compare.
	b, err := MarshalItem(it)
	if err != nil {
		t.Fatal(err)
	}
	var back Item
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(it, back) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", back, it)
	}
}

func TestWriteReadItemsJSONL(t *testing.T) {
	items := []Item{
		{ID: "a", GoldPoints: []string{}, GoldRefs: []string{}},
		{ID: "b", GoldPoints: []string{"x"}, GoldRefs: []string{"lov/2002-06-21-34#§16"}},
	}
	var buf bytes.Buffer
	if err := WriteItemsJSONL(&buf, items); err != nil {
		t.Fatal(err)
	}
	back, err := ReadItemsJSONL(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0].ID != "a" || back[1].ID != "b" {
		t.Fatalf("got %+v", back)
	}
	if back[1].GoldRefs[0] != "lov/2002-06-21-34#§16" {
		t.Fatalf("gold refs = %v", back[1].GoldRefs)
	}
}

func TestNewItemDefaults(t *testing.T) {
	it := NewItem()
	if it.Source != "uio" || it.Difficulty != "unknown" || it.License != "uio-public" || !it.NeedsCuration {
		t.Fatalf("defaults wrong: %+v", it)
	}
	if it.GoldPoints == nil || it.GoldRefs == nil {
		t.Fatalf("gold slices must be non-nil: %+v", it)
	}
}
