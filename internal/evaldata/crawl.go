package evaldata

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Link is a single <a href> extracted from an HTML document, along with its
// visible text (the anchor's inner text).
type Link struct {
	Href string
	Text string
}

// ParseLinks extracts every <a href> from raw HTML, returning the absolute
// href (resolved against base) and the anchor's visible text.
func ParseLinks(raw []byte, base string) ([]Link, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	var links []Link
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			var href string
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = a.Val
					break
				}
			}
			if href != "" {
				links = append(links, Link{
					Href: ResolveURL(baseURL, href),
					Text: strings.TrimSpace(textContent(n)),
				})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links, nil
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// ResolveURL resolves href against base and normalises it: scheme is forced to
// https, and the host is canonicalised (bare uio.no -> www.uio.no,
// www.jus.uio.no -> jus.uio.no). Note that www.uio.no and jus.uio.no are NOT
// aliases: www.uio.no serves the /emner/ tree, jus.uio.no serves the
// /studier/ressurser/tidligere-eksamen/ tree, so both are preserved.
func ResolveURL(base *url.URL, href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	resolved := base.ResolveReference(ref)
	resolved.Scheme = "https"
	resolved.Host = canonicalHost(resolved.Host)
	return resolved.String()
}

// NormalizeLink is a string-in/string-out convenience wrapper around ResolveURL.
func NormalizeLink(base, href string) string {
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	return ResolveURL(b, href)
}

func canonicalHost(host string) string {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	switch h {
	case "uio.no":
		return "www.uio.no"
	case "www.jus.uio.no":
		return "jus.uio.no"
	default:
		return h
	}
}

// isUioHost reports whether the URL is on the UiO domain.
func isUioHost(u *url.URL) bool {
	h := u.Hostname()
	return h == "uio.no" || strings.HasSuffix(h, ".uio.no")
}

// IsFollowable reports whether a page URL should be crawled. Two classes:
//
//  1. course "oppgaver" pages: path contains /oppgaver/, tidligere-eksamensoppgaver,
//     or previous-exam-papers (any casing), on the UiO domain;
//  2. navigation/index pages under the tidligere-eksamen/Jus tree (study-year,
//     valgemner, eldre-studieordninger and their sub-indices).
func IsFollowable(rawURL string) bool {
	if IsDocument(rawURL) {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !isUioHost(u) {
		return false
	}
	p := strings.ToLower(u.Path)
	if strings.Contains(p, "/oppgaver/") ||
		strings.Contains(p, "tidligere-eksamensoppgaver") ||
		strings.Contains(p, "previous-exam-papers") ||
		strings.Contains(p, "previous%20exam%20papers") {
		return true
	}
	return strings.Contains(p, "tidligere-eksamen/jus")
}

// IsDocument reports whether a link points to a downloadable exam document
// (PDF or DOCX), which we cache and later extract.
func IsDocument(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	return strings.HasSuffix(p, ".pdf") || strings.HasSuffix(p, ".docx")
}

// Filename returns the URL-escaped-decoded basename of a document URL, used as
// the on-disk cache filename.
func Filename(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	base := u.Path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if decoded, err := url.PathUnescape(base); err == nil {
		return decoded
	}
	return base
}

// courseCodeRE matches the course code segment in emner paths, e.g. JUS1111,
// JUR1000, TYSJUR1, JFEXFAC04. The code sits between /jus/jus/ and the next /.
var courseCodeRE = regexp.MustCompile(`/emner/jus/jus/([A-Za-z0-9]+)/`)

// fallbackCourseRE matches any course-like token (2+ uppercase letters followed
// by 2–4 digits, optionally a trailing letter) anywhere in the path.
var fallbackCourseRE = regexp.MustCompile(`([A-Z]{2,}[0-9]{2,4}[A-Za-z]?)`)

// CourseFromURL extracts the course code (e.g. JUS1111) from an oppgaver page
// URL. It returns "" when no course code can be found.
func CourseFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := u.Path
	if m := courseCodeRE.FindStringSubmatch(path); m != nil {
		return strings.ToUpper(m[1])
	}
	if m := fallbackCourseRE.FindStringSubmatch(path); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// semesterRE matches a semester token "v26" / "h25" (case-insensitive). Older
// papers without a semester token yield "".
var semesterRE = regexp.MustCompile(`(?i)[vh][0-9]{2}`)

// extractSemester finds the first semester token whose neighbours are not
// letters/digits (so "v26" is matched but "v26b" and "av26" are not).
func extractSemester(s string) string {
	for _, loc := range semesterRE.FindAllStringIndex(s, -1) {
		if loc[0] > 0 && isAlphaNum(s[loc[0]-1]) {
			continue
		}
		if loc[1] < len(s) && isAlphaNum(s[loc[1]]) {
			continue
		}
		return strings.ToLower(s[loc[0]:loc[1]])
	}
	return ""
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// ClassifyDoc determines kind ("oppgave"/"veiledning"), semester ("v26"), and
// language ("nb"/"nn") from a document URL and its anchor text. The filename is
// authoritative; the anchor text is a fallback for kind/language.
func ClassifyDoc(rawURL, anchorText string) (kind, semester, language string) {
	fn := strings.ToLower(Filename(rawURL))
	text := strings.ToLower(anchorText)

	// Kind.
	switch {
	case strings.Contains(fn, "sensorveiledning") || strings.Contains(fn, "sens_veiledning"):
		kind = "veiledning"
	case strings.Contains(fn, "eksamensoppg") || strings.Contains(fn, "eksamensopg"):
		kind = "oppgave"
	case strings.Contains(text, "sensorveiledning"):
		kind = "veiledning"
	case strings.Contains(text, "eksamensoppg"):
		kind = "oppgave"
	default:
		kind = "oppgave"
	}

	// Semester from the filename (fall back to anchor text).
	if m := extractSemester(fn); m != "" {
		semester = m
	} else if m := extractSemester(text); m != "" {
		semester = m
	}

	// Language.
	switch {
	case strings.Contains(fn, "nynorsk") || strings.Contains(text, "nynorsk"):
		language = "nn"
	case strings.Contains(fn, "bokmal") || strings.Contains(fn, "bokmål") ||
		strings.Contains(text, "bokmål") || strings.Contains(text, "bokmal"):
		language = "nb"
	default:
		language = "nb"
	}
	return kind, semester, language
}

// fetchHTML retrieves a URL and returns its body, enforcing a success status.
func fetchHTML(client *http.Client, rawURL string) ([]byte, error) {
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MiB cap
}
