package evaldata

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Source is a document source registry entry.
type Source struct {
	Name string
	// SeedURLs are the crawl entry points. For UiO a single top index is
	// sufficient because the crawler follows the tidligere-eksamen/Jus tree
	// recursively (bounded).
	SeedURLs []string
}

// UIOSource mirrors evals/sources.yaml. Keep in sync with that file.
var UIOSource = Source{
	Name: "uio",
	SeedURLs: []string{
		"https://www.jus.uio.no/studier/ressurser/tidligere-eksamen/Jus/index.html",
	},
}

// CrawlOptions bounds a crawl and controls HTTP behaviour.
type CrawlOptions struct {
	Client   *http.Client // nil -> a default client with a 30s timeout
	MaxPages int          // max HTML pages to visit (0 = 500 default)
	Limit    int          // max documents to collect (0 = unlimited)
	Logf     func(format string, args ...interface{})
}

// FetchedDoc is one discovered exam document (oppgave or veiledning).
type FetchedDoc struct {
	URL       string
	Course    string
	Semester  string
	Kind      string // "oppgave" | "veiledning"
	Language  string // "nb" | "nn"
	SourceURL string // the oppgaver page it was found on
}

// CrawlSource performs a bounded BFS from the source's seed URLs and returns
// the discovered exam documents, deduplicated by URL.
func CrawlSource(src Source, opts CrawlOptions) ([]FetchedDoc, error) {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	maxPages := opts.MaxPages
	if maxPages == 0 {
		maxPages = 500
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}

	queue := append([]string(nil), src.SeedURLs...)
	visited := map[string]bool{}
	seenDoc := map[string]bool{}
	var docs []FetchedDoc
	pages := 0

	for len(queue) > 0 && pages < maxPages {
		pageURL := queue[0]
		queue = queue[1:]
		if visited[pageURL] {
			continue
		}
		visited[pageURL] = true
		pages++

		body, err := fetchHTML(client, pageURL)
		if err != nil {
			logf("warn: %v", err)
			continue
		}
		links, err := ParseLinks(body, pageURL)
		if err != nil {
			logf("warn: parse %s: %v", pageURL, err)
			continue
		}

		isLawPage := isLawCoursePage(pageURL)
		for _, l := range links {
			switch {
			case IsDocument(l.Href):
				if !isLawPage {
					continue
				}
				if seenDoc[l.Href] {
					continue
				}
				kind, semester, language := ClassifyDoc(l.Href, l.Text)
				course := CourseFromURL(pageURL)
				if course == "" {
					continue
				}
				seenDoc[l.Href] = true
				docs = append(docs, FetchedDoc{
					URL:       l.Href,
					Course:    course,
					Semester:  semester,
					Kind:      kind,
					Language:  language,
					SourceURL: pageURL,
				})
				if opts.Limit > 0 && len(docs) >= opts.Limit {
					return docs, nil
				}
			case IsFollowable(l.Href):
				if !visited[l.Href] {
					queue = append(queue, l.Href)
				}
			}
		}
	}
	return docs, nil
}

// isLawCoursePage reports whether a page URL is a law-faculty course page
// (under /emner/jus/jus/). This excludes e.g. the EXPHIL03 philosophy course
// that also matches the tidligere-eksamensoppgaver follow rule.
func isLawCoursePage(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(u.Path), "/emner/jus/jus/")
}

// DeduplicateDocs removes documents sharing the same URL (keeps first).
func DeduplicateDocs(docs []FetchedDoc) []FetchedDoc {
	seen := map[string]bool{}
	out := make([]FetchedDoc, 0, len(docs))
	for _, d := range docs {
		if seen[d.URL] {
			continue
		}
		seen[d.URL] = true
		out = append(out, d)
	}
	return out
}

// ValidateFetchedDoc reports a human-readable problem with a document, or "".
// Used to surface silent data-quality issues during fetch without failing.
func ValidateFetchedDoc(d FetchedDoc) string {
	switch {
	case d.Course == "":
		return "missing course"
	case d.Semester == "":
		return "missing semester"
	case d.Kind == "":
		return "missing kind"
	}
	return ""
}
