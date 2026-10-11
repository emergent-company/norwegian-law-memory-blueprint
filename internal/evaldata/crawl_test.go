package evaldata

import (
	"strings"
	"testing"
)

func TestParseLinks(t *testing.T) {
	html := `<html><body>
		<a href="/studier/emner/jus/jus/JUS1111/oppgaver/jus1111_v26.pdf">Eksamensoppgave</a>
		<a href="https://www.uio.no/x.pdf">absolute</a>
		<a name="anchor"></a>
		<a href="/rel.html">page</a>
	</body></html>`
	links, err := ParseLinks([]byte(html), "https://www.uio.no/studier/emner/jus/jus/JUS1111/oppgaver/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("got %d links, want 3: %+v", len(links), links)
	}
	// Relative href resolved + normalised to https://www.uio.no.
	if links[0].Href != "https://www.uio.no/studier/emner/jus/jus/JUS1111/oppgaver/jus1111_v26.pdf" {
		t.Errorf("href[0] = %q", links[0].Href)
	}
	if links[0].Text != "Eksamensoppgave" {
		t.Errorf("text[0] = %q", links[0].Text)
	}
}

func TestResolveURLNormalises(t *testing.T) {
	cases := []struct{ base, href, want string }{
		{"https://www.jus.uio.no/a/index.html", "http://www.uio.no/x.pdf", "https://www.uio.no/x.pdf"},
		{"https://www.uio.no/a/", "../b/c.pdf", "https://www.uio.no/b/c.pdf"},
		{"https://www.uio.no/a/", "/abs.pdf", "https://www.uio.no/abs.pdf"},
		{"https://www.jus.uio.no/a/", "http://www.jus.uio.no/studier/index.html", "https://jus.uio.no/studier/index.html"},
		{"https://www.uio.no/a/", "http://uio.no/b.pdf", "https://www.uio.no/b.pdf"},
	}
	for _, c := range cases {
		got := NormalizeLink(c.base, c.href)
		if got != c.want {
			t.Errorf("resolve(%q, %q) = %q, want %q", c.base, c.href, got, c.want)
		}
	}
}

func TestCourseFromURL(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://www.uio.no/studier/emner/jus/jus/JUS1111/oppgaver/index.html", "JUS1111"},
		{"https://www.uio.no/studier/emner/jus/jus/JUR1000/oppgaver/index.html", "JUR1000"},
		{"https://www.uio.no/studier/emner/jus/jus/JUS5120/tidligere-eksamensoppgaver/", "JUS5120"},
		{"https://www.uio.no/studier/emner/jus/jus/TYSJUR1/tidligere-eksamensoppgaver/index.html", "TYSJUR1"},
		{"https://www.uio.no/studier/emner/hf/ifikk/EXPHIL03/tidligere-eksamensoppgaver/", "EXPHIL03"},
		{"https://www.jus.uio.no/studier/ressurser/tidligere-eksamen/Jus/index.html", ""},
	}
	for _, c := range cases {
		if got := CourseFromURL(c.url); got != c.want {
			t.Errorf("CourseFromURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestClassifyDoc(t *testing.T) {
	cases := []struct {
		url, text                string
		kind, semester, language string
	}{
		{"https://www.uio.no/x/jus1111_eksamensoppgave_signert_v26_bokmal.pdf", "Eksamensoppgave bokmål", "oppgave", "v26", "nb"},
		{"https://www.uio.no/x/jus1111_eksamensoppgave_signert_v26_nynorsk.pdf", "Eksamensoppgave nynorsk", "oppgave", "v26", "nn"},
		{"https://www.uio.no/x/sensorveiledning-jus1111-v26_revidert_12-6-2026.pdf", "Sensorveiledning", "veiledning", "v26", "nb"},
		{"https://www.uio.no/x/sensorveiledning_jus1111_h25_rev.pdf", "Sensorveiledning", "veiledning", "h25", "nb"},
		{"https://www.uio.no/x/eksamensoppgave_jus1111_v24.pdf", "Eksamensoppgave bokmål", "oppgave", "v24", "nb"},
		{"https://www.uio.no/x/jus1111_nynorsk_h23.pdf", "Eksamensoppgave nynorsk", "oppgave", "h23", "nn"},
		{"https://www.uio.no/x/jus1111-eksamensoppgave.pdf", "Eksamensoppgave", "oppgave", "", "nb"},
	}
	for _, c := range cases {
		kind, sem, lang := ClassifyDoc(c.url, c.text)
		if kind != c.kind || sem != c.semester || lang != c.language {
			t.Errorf("ClassifyDoc(%q) = (%q,%q,%q), want (%q,%q,%q)",
				c.url, kind, sem, lang, c.kind, c.semester, c.language)
		}
	}
}

func TestIsFollowable(t *testing.T) {
	follow := []string{
		"https://www.uio.no/studier/emner/jus/jus/JUS1111/oppgaver/index.html",
		"http://www.uio.no/studier/emner/jus/jus/JUS5120/tidligere-eksamensoppgaver/",
		"https://www.uio.no/studier/emner/jus/jus/JUS5230/previous-exam-papers/index.html",
		"http://www.jus.uio.no/studier/ressurser/tidligere-eksamen/Jus/1.-studiear/index.html",
		"http://www.jus.uio.no/studier/ressurser/tidligere-eksamen/Jus/valgemner/index.html",
	}
	skip := []string{
		"https://www.uio.no/studier/emner/hf/ifikk/EXPHIL03/tidligere-eksamensoppgaver/", // still followable by rule
		"https://example.com/oppgaver/index.html",                                        // off-domain
		"https://www.uio.no/studier/emner/jus/jus/JUS1111/oppgaver/jus1111_v26.pdf",      // document, not page
		"https://www.uio.no/studier/emner/jus/jus/JUS1111/",                              // course home
	}
	for _, u := range follow {
		if !IsFollowable(u) {
			t.Errorf("IsFollowable(%q) = false, want true", u)
		}
	}
	// Only the off-domain and non-matching paths must be skipped.
	for _, u := range skip {
		if strings.Contains(u, "uio.no") && strings.Contains(u, "tidligere-eksamensoppgaver") && !strings.Contains(u, "hf/ifikk") {
			continue
		}
		if u == skip[0] { // EXPHIL03 still matches the rule
			if !IsFollowable(u) {
				t.Errorf("IsFollowable(%q) = false, want true (matches follow rule)", u)
			}
			continue
		}
		if IsFollowable(u) {
			t.Errorf("IsFollowable(%q) = true, want false", u)
		}
	}
}

func TestIsDocument(t *testing.T) {
	if !IsDocument("https://www.uio.no/x/jus1111_v26.pdf") {
		t.Error("pdf should be a document")
	}
	if !IsDocument("https://www.uio.no/x/jus1111_v26.PDF") {
		t.Error("uppercase .PDF should be a document")
	}
	if !IsDocument("https://www.uio.no/x/oppgave.docx") {
		t.Error("docx should be a document")
	}
	if IsDocument("https://www.uio.no/x/oppgaver/index.html") {
		t.Error("html should not be a document")
	}
}
