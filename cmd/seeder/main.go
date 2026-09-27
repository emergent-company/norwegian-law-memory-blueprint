package main

// Norwegian Law Memory Blueprint Seeder
//
// Downloads or uses cached Lovdata archives, parses Norwegian law HTML,
// fetches EU directive metadata from EUR-Lex and EuroVoc SPARQL, then
// bulk-ingests everything into a Memory project via the SDK.
//
// Data sources:
//   - Lovdata public datasets (NLOD 2.0): gjeldende-lover.tar.bz2, gjeldende-sentrale-forskrifter.tar.bz2
//   - EUR-Lex HTML (public): full metadata for every EU directive referenced by Norwegian law
//   - EuroVoc SPARQL (EU Publications Office, public): English labels for EuroVoc concepts
//
// Usage:
//   ./seeder --server http://localhost:3012 --token <token> --project <id>
//   ./seeder --ingest-only --limit 50 --skip-eu    # smoke test with cached data
//
// Environment variables (all overridable by flags):
//   MEMORY_SERVER          server URL
//   MEMORY_PROJECT_TOKEN   project API token
//   MEMORY_PROJECT_ID      project ID
//   MEMORY_STATE_DIR       checkpoint directory
//   SEED_LIMIT             max documents per dataset

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/bzip2"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	"golang.org/x/net/html"
)

// ─── Phase constants ──────────────────────────────────────────────────────────

const (
	phaseObjectsPending = "objects_pending"
	phaseObjectsDone    = "objects_done"
	phaseRelsPending    = "rels_pending"
	phaseDone           = "done"
)

// ─── Data URLs ────────────────────────────────────────────────────────────────

const (
	defaultCacheDir = "/tmp/lovdata_data"
	lawsURL         = "https://api.lovdata.no/v1/publicData/get/gjeldende-lover.tar.bz2"
	regsURL         = "https://api.lovdata.no/v1/publicData/get/gjeldende-sentrale-forskrifter.tar.bz2"
	eurLexBase      = "https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=CELEX:"
	eurovocSPQL     = "https://publications.europa.eu/webapi/rdf/sparql"
	cellarSPQL      = "https://publications.europa.eu/webapi/rdf/sparql"
	cellarBase      = "https://publications.europa.eu/resource/cellar/"
)

// ─── Config ───────────────────────────────────────────────────────────────────

type config struct {
	serverURL    string
	token        string
	projectID    string
	orgID        string
	projectName  string
	stateDir     string
	cacheDir     string
	limit        int
	skipEU       bool
	euLimit      int
	workers      int
	batchSz      int
	ingestOnly   bool
	downloadOnly bool
	cleanup      bool
	dataset      string // "laws", "regulations", "both"
	dumpSeedDir  string // "" = upload mode; non-empty = server-free seed export
}

func envOr(envKey, defaultVal string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultVal
}

func envIntOr(envKey string, defaultVal int) int {
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func parseConfig() config {
	defaultStateDir := filepath.Join(os.Getenv("HOME"), ".norwegian-law-seed-state")

	serverURL := flag.String("server", envOr("MEMORY_SERVER", ""), "Memory server URL (required)")
	token := flag.String("token", envOr("MEMORY_PROJECT_TOKEN", ""), "Project API token (required)")
	projectID := flag.String("project", envOr("MEMORY_PROJECT_ID", ""), "Project ID (optional; overrides name-based auto-resolve)")
	orgID := flag.String("org-id", envOr("MEMORY_ORG_ID", ""), "Organisation ID for project creation (default: user's org)")
	projectName := flag.String("project-name", envOr("NORWEGIAN_LAW_PROJECT_NAME", "Norwegian Law"), "Project name to find or create")
	stateDir := flag.String("state-dir", envOr("MEMORY_STATE_DIR", defaultStateDir), "Checkpoint directory")
	cacheDir := flag.String("cache-dir", envOr("LOVDATA_CACHE_DIR", defaultCacheDir), "Data cache directory")
	limit := flag.Int("limit", envIntOr("SEED_LIMIT", 0), "Max documents per dataset (0 = no limit)")
	skipEU := flag.Bool("skip-eu", false, "Skip EUR-Lex and EuroVoc enrichment")
	euLimit := flag.Int("eu-limit", 0, "Max EU directives to fetch (0 = all)")
	workers := flag.Int("workers", 20, "Parallel upload workers")
	batchSz := flag.Int("batch", 100, "Batch size for bulk API calls (max 100)")
	ingestOnly := flag.Bool("ingest-only", false, "Skip download; use cached parsed JSON")
	downloadOnly := flag.Bool("download-only", false, "Download and parse to cache, skip ingestion")
	cleanup := flag.Bool("cleanup", false, "Delete the target project after the run")
	dataset := flag.String("dataset", "both", `Dataset to import: "laws", "regulations", or "both"`)
	dumpSeedDir := flag.String("dump-seed", envOr("SEED_DUMP_DIR", ""), "Write a server-free blueprint seed (JSONL) to this directory instead of uploading")

	flag.Parse()

	sz := *batchSz
	if sz > 100 {
		log.Printf("Note: max batch size is 100; capping from %d", sz)
		sz = 100
	}

	return config{
		serverURL:    *serverURL,
		token:        *token,
		projectID:    *projectID,
		orgID:        *orgID,
		projectName:  *projectName,
		stateDir:     *stateDir,
		cacheDir:     *cacheDir,
		limit:        *limit,
		skipEU:       *skipEU,
		euLimit:      *euLimit,
		workers:      *workers,
		batchSz:      sz,
		ingestOnly:   *ingestOnly,
		downloadOnly: *downloadOnly,
		cleanup:      *cleanup,
		dataset:      *dataset,
		dumpSeedDir:  *dumpSeedDir,
	}
}

func (c *config) validate() error {
	var missing []string
	if c.serverURL == "" {
		missing = append(missing, "--server / MEMORY_SERVER")
	}
	if c.token == "" {
		missing = append(missing, "--token / MEMORY_PROJECT_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required parameters: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ─── State persistence ────────────────────────────────────────────────────────

type SeedState struct {
	Phase string `json:"phase"`
}

func loadState(dir string) SeedState {
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return SeedState{Phase: phaseObjectsPending}
	}
	var s SeedState
	if err := json.Unmarshal(data, &s); err != nil {
		return SeedState{Phase: phaseObjectsPending}
	}
	return s
}

func saveState(dir string, s SeedState) {
	os.MkdirAll(dir, 0755) //nolint:errcheck
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(filepath.Join(dir, "state.json"), data, 0644) //nolint:errcheck
}

func loadIDMap(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "idmap.json"))
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveIDMap(dir string, idMap map[string]string) {
	os.MkdirAll(dir, 0755) //nolint:errcheck
	data, _ := json.Marshal(idMap)
	os.WriteFile(filepath.Join(dir, "idmap.json"), data, 0644) //nolint:errcheck
}

func loadRelsDone(dir string) map[int]bool {
	done := make(map[int]bool)
	f, err := os.Open(filepath.Join(dir, "rels_done.txt"))
	if err != nil {
		return done
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if idx, err := strconv.Atoi(line); err == nil {
			done[idx] = true
		}
	}
	return done
}

func appendRelDone(dir string, idx int) {
	f, err := os.OpenFile(filepath.Join(dir, "rels_done.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%d\n", idx)
}

func appendRelFailed(dir string, items []graph.CreateRelationshipRequest) {
	f, err := os.OpenFile(filepath.Join(dir, "rels_failed.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	data, _ := json.Marshal(items)
	f.Write(data)         //nolint:errcheck
	f.Write([]byte("\n")) //nolint:errcheck
}

// ─── Data types ───────────────────────────────────────────────────────────────

type LovDoc struct {
	RefID             string
	DocID             string
	LegacyID          string
	Title             string
	ShortTitle        string
	Language          string
	Ministry          string
	LegalArea         string
	LegalSubArea      string
	AllLegalAreas     []string
	AllLegalSubAreas  []string
	DateInForce       string
	LastChangeInForce string
	DateOfPublication string
	AppliesTo         string
	LastChangedByRef  string
	AmendsRefs        []string
	SeeAlsoRefs       []string
	EEAReferences     string
	EUDirectiveIDs    []string
	DocType           string // "Law" | "Regulation"
	References        []string
	Content           string // Full Markdown of law body
	Paragraphs        []LovParagraph
	EUBodyRefs        []string // eu/XXXXXXXX hrefs from body → CITES_EU_LAW edges
	SourceSHA256      string   // SHA-256 of the raw source XML bytes (change-detection key)
}

type LovParagraph struct {
	SectionID    string
	ChapterID    string
	ParagraphNum string
	Title        string
	Content      string
	Position     int
}

type EUDirective struct {
	DirectiveID     string
	CelexID         string
	FullTitle       string
	ShortTitle      string
	Form            string
	DateOfDocument  string
	DateOfEffect    string
	Author          string
	ResponsibleDG   string
	Content         string
	SubjectMatter   string
	DirectoryCode   string
	LegalBasis      string
	ProcedureNum    string
	OJReference     string
	EuroVocIDs      []string
	CitedCELEX      []string
	ModifiedByCELEX []string
}

type EuroVocConcept struct {
	ID      string
	LabelEN string
}

// ─── Regex ────────────────────────────────────────────────────────────────────

var (
	refPattern       = regexp.MustCompile(`^(?:lov|forskrift|res)/\d{4}-\d{2}-\d{2}`)
	directivePattern = regexp.MustCompile(`\b(\d{4}/[\d]+/(?:EF|EØF|EU|EEC|EC|EØF))\b`)
	celexPattern     = regexp.MustCompile(`\b(3\d{7}[A-Z]\d+)\b`)
	eurovocPattern   = regexp.MustCompile(`eurovoc\.europa\.eu/(\d+)`)
	// romanOnlyPattern matches a bare Roman numeral (I, II, III, …). Callers
	// strip a trailing period ("I.") before matching. The pattern deliberately
	// has no optional-period suffix: with an all-optional Roman expression that
	// would also match "." alone. Lovdata uses such <h2> headings as section
	// dividers, not chapter titles, so they must not be rendered as
	// "## Kapittel <n>. <roman>".
	romanOnlyPattern = regexp.MustCompile(`(?i)^M{0,4}(CM|CD|D?C{0,3})(XC|XL|L?X{0,3})(IX|IV|V?I{0,3})$`)
	// delPattern matches a "Del" (part) heading, including the spelled-out
	// ordinal forms ("Første del", "Andre del", …) and the definite form
	// ("delen", "Første delen"). Lovdata labels parts as chapters in <section>
	// wrappers, but the title is a part, not a chapter, so it must be kept as
	// authored rather than prefixed with "Kapittel".
	//
	// The boundary is a negated letter class rather than \b: Go's \b is
	// ASCII-only, so it would treat "å" as a non-word char and wrongly match
	// non-part titles like "Delårsregnskap". Requiring the following char to be
	// a non-letter (or end-of-string) rejects "Delårsregnskap"/"Deling"/
	// "Delegering"/"Deltakelse" while still matching "Del I."/"Første delen".
	delPattern = regexp.MustCompile(`(?i)^(del(en)?|(første|andre|tredje|fjerde|femte|sjette|sjuende|syvende|åttende|niende|tiende|ellevte|tolvte)\s+del(en)?)([^a-zA-ZæøåÆØÅ]|$)`)
)

// ─── HTML helpers ─────────────────────────────────────────────────────────────

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func findAll(n *html.Node, tag string) []*html.Node {
	var results []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			results = append(results, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return results
}

func findFirst(n *html.Node, tag string) *html.Node {
	var result *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if result != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == tag {
			result = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return result
}

func appendUniq(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func stripAnchor(s string) string {
	if idx := strings.Index(s, "#"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func strPtr(s string) *string { return &s }

// isBodyArticleClass reports whether an <article> element's class marks it as a
// § (article) container, as opposed to a bare paragraph. Lovdata has two names
// for the same thing depending on document vintage.
func isBodyArticleClass(cls string) bool {
	return cls == "legalArticle" || cls == "futureLegalArticle"
}

// isParagraphClass reports whether a node's class marks it as a body paragraph.
func isParagraphClass(cls string) bool {
	switch cls {
	case "legalP", "numberedLegalP", "defaultP", "centeredP":
		return true
	}
	return false
}

// ─── Body extraction ──────────────────────────────────────────────────────────

// extractBody walks <main id="dokument"> and returns the full body Markdown, one
// LegalParagraph chunk per § article, and any EU cross-reference codes.
//
// The Lovdata HTML class taxonomy follows the proven reference parser
// https://github.com/sondreskarsten/norwegian-laws
// (lovdata-loader/src/lovdata_loader/parser.py, MIT): a § is an
// <article class="legalArticle"> (or the newer "futureLegalArticle"), while its
// body paragraphs are <article>/<p> elements carrying class "legalP",
// "numberedLegalP", "defaultP" or "centeredP" (plus the legacy
// "legalPfortsettelse"/"leddfortsettelse" continuation variants). Short
// delegation regulations skip the legalArticle wrapper entirely and place
// <article class="legalP"> directly under <section>; those are captured by the
// top-level legalP fallback below.
func extractBody(main *html.Node) (fullMarkdown string, paragraphs []LovParagraph, euBodyRefs []string) {
	var sb strings.Builder
	euRefSet := make(map[string]bool)
	position := 0
	currentChapterID := ""

	skipClasses := map[string]bool{
		"changesToParent": true,
		"footnotes":       true,
		"tocSubUl":        true,
	}

	var extractPlainText func(*html.Node) string
	extractPlainText = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		if n.Type == html.ElementNode && skipClasses[attr(n, "class")] {
			return ""
		}
		var parts []string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			parts = append(parts, extractPlainText(c))
		}
		return strings.Join(parts, "")
	}

	var collectEURefs func(*html.Node)
	collectEURefs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := strings.TrimPrefix(attr(n, "href"), "/")
			href = stripAnchor(href)
			if strings.HasPrefix(href, "eu/") && len(href) > 3 {
				code := strings.ToUpper(href[3:])
				euRefSet[code] = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectEURefs(c)
		}
	}

	renderOL := func(ol *html.Node) string {
		var olsb strings.Builder
		i := 1
		for c := ol.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "li" {
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					olsb.WriteString(fmt.Sprintf("%d. %s\n", i, text))
					i++
				}
			}
		}
		return olsb.String()
	}

	renderUL := func(ul *html.Node) string {
		var ulsb strings.Builder
		for c := ul.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "li" {
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					ulsb.WriteString("- " + text + "\n")
				}
			}
		}
		return ulsb.String()
	}

	renderArticleContent := func(n *html.Node) string {
		var asb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			cls := attr(c, "class")
			if skipClasses[cls] {
				continue
			}
			switch cls {
			case "legalArticleHeader", "futureLegalArticleHeader":
				continue
			case "numberedLegalP", "legalP", "defaultP", "centeredP", "legalPfortsettelse", "leddfortsettelse":
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					asb.WriteString(text + "\n\n")
				}
			case "listArticle":
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					asb.WriteString("- " + text + "\n")
				}
			default:
				if c.Data == "ol" {
					asb.WriteString(renderOL(c))
				} else if c.Data == "ul" && cls != "tocSubUl" {
					asb.WriteString(renderUL(c))
				} else {
					text := strings.TrimSpace(extractPlainText(c))
					if text != "" {
						asb.WriteString(text + "\n\n")
					}
				}
			}
		}
		return asb.String()
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		cls := attr(n, "class")
		aid := attr(n, "id")

		if skipClasses[cls] {
			return
		}

		switch {
		case n.Data == "section":
			// Capture/restore the current chapter id so nested <section> elements
			// don't leak their inner id to siblings that follow after they close.
			prevChapterID := currentChapterID
			currentChapterID = aid
			chNum := ""
			if parts := strings.Split(aid, "-"); len(parts) >= 2 {
				chNum = parts[len(parts)-1]
			}
			chTitle := ""
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "h2" || c.Data == "h1") {
					chTitle = strings.TrimSpace(extractPlainText(c))
					break
				}
			}
			// A bare Roman numeral heading ("I", "II", "I.") is a section divider,
			// not a chapter title — suppress the bogus "## Kapittel <n>. <roman>".
			// Strip one trailing period first so a lone "." is not treated as Roman.
			romanCore := strings.TrimSuffix(chTitle, ".")
			isRomanOnly := romanCore != "" && romanOnlyPattern.MatchString(romanCore)
			if !isRomanOnly && (chNum != "" || chTitle != "") {
				lower := strings.ToLower(chTitle)
				switch {
				case chTitle != "" && strings.HasPrefix(lower, "kapittel"):
					// Title already carries the "Kapittel" prefix — emit as-is to
					// avoid "## Kapittel 1. Kapittel 1. …" duplication.
					sb.WriteString("\n## " + chTitle + "\n\n")
				case chTitle != "" && delPattern.MatchString(chTitle):
					// "Del"/"Første del" headings are parts, not chapters — keep
					// them as authored instead of mislabeling them as chapters.
					sb.WriteString("\n## " + chTitle + "\n\n")
				default:
					heading := "## Kapittel " + chNum
					if chTitle != "" {
						heading += ". " + chTitle
					}
					sb.WriteString("\n" + heading + "\n\n")
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			currentChapterID = prevChapterID

		case n.Data == "article" && isBodyArticleClass(cls):
			position++
			sectionID := aid

			paraNum := ""
			paraTitle := ""
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode {
					continue
				}
				hcls := attr(c, "class")
				// Lovdata marks the § header with class "legalArticleHeader" on
				// any heading level (h2–h6, and occasionally a div), plus the newer
				// "futureLegalArticleHeader" span form. Match the CLASS, not the
				// tag, so h4 (e.g. skatteloven) and the future form are captured.
				if hcls != "legalArticleHeader" && hcls != "futureLegalArticleHeader" {
					continue
				}
				// Search the header's subtree (bounded to this element) for the
				// value/title spans so nesting differences don't drop them.
				for _, span := range findAll(c, "span") {
					switch attr(span, "class") {
					case "legalArticleValue":
						if paraNum == "" {
							paraNum = strings.TrimSpace(extractPlainText(span))
						}
					case "legalArticleTitle":
						if paraTitle == "" {
							paraTitle = strings.TrimSpace(extractPlainText(span))
						}
					}
				}
				break
			}

			// Build the heading from non-empty parts only; emit no stray "### "
			// when both the number and title are empty (the body row is kept).
			var heading string
			switch {
			case paraNum != "" && paraTitle != "":
				heading = "### " + paraNum + " — " + paraTitle
			case paraNum != "":
				heading = "### " + paraNum
			case paraTitle != "":
				heading = "### " + paraTitle
			}
			if heading != "" {
				sb.WriteString(heading + "\n\n")
			}

			bodyText := renderArticleContent(n)
			sb.WriteString(bodyText)

			collectEURefs(n)

			paraContent := strings.TrimSpace(heading + "\n\n" + bodyText)
			if paraContent != "" && sectionID != "" {
				paragraphs = append(paragraphs, LovParagraph{
					SectionID:    sectionID,
					ChapterID:    currentChapterID,
					ParagraphNum: paraNum,
					Title:        paraTitle,
					Content:      paraContent,
					Position:     position,
				})
			}

		case n.Data == "article" && isParagraphClass(cls):
			// Top-level paragraph fallback: short/delegation regulations put body
			// text in <article class="legalP"> directly under <section>, with no
			// enclosing legalArticle. Capture the text into the body and, where an
			// id exists, emit it as a LegalParagraph chunk keyed by that id.
			position++
			text := strings.TrimSpace(extractPlainText(n))
			if text != "" {
				sb.WriteString(text + "\n\n")
			}
			collectEURefs(n)
			if text != "" && aid != "" {
				paragraphs = append(paragraphs, LovParagraph{
					SectionID:    aid,
					ChapterID:    currentChapterID,
					ParagraphNum: "",
					Title:        "",
					Content:      text,
					Position:     position,
				})
			}

		case n.Data == "ol":
			sb.WriteString(renderOL(n))

		case n.Data == "ul" && cls != "tocSubUl":
			sb.WriteString(renderUL(n))

		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}

	for c := main.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	collectEURefs(main)

	fullMarkdown = strings.TrimSpace(sb.String())
	for code := range euRefSet {
		euBodyRefs = append(euBodyRefs, code)
	}
	sort.Strings(euBodyRefs)
	return fullMarkdown, paragraphs, euBodyRefs
}

// ─── Document parser ──────────────────────────────────────────────────────────

func parseDocument(content []byte, docType string) *LovDoc {
	// Hash the raw source bytes as read (the tar member XML), not any derived or
	// rendered form, so re-exports are change-detected against the actual source.
	doc := &LovDoc{DocType: docType, SourceSHA256: sha256Hex(content)}

	// Detect language from <html lang="...">
	if idx := strings.Index(string(content), `lang="`); idx >= 0 {
		rest := string(content)[idx+6:]
		if end := strings.Index(rest, `"`); end > 0 {
			doc.Language = rest[:end]
		}
	}

	root, err := html.Parse(strings.NewReader(string(content)))
	if err != nil {
		return nil
	}

	var currentDtClass string
	var walkMeta func(*html.Node)
	walkMeta = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "dt":
				currentDtClass = attr(n, "class")
			case "dd":
				cls := attr(n, "class")
				if cls != currentDtClass {
					break
				}
				text := strings.TrimSpace(nodeText(n))
				switch cls {
				case "refid":
					doc.RefID = text
				case "dokid":
					doc.DocID = text
				case "legacyID":
					doc.LegacyID = text
				case "title":
					doc.Title = text
				case "titleShort":
					doc.ShortTitle = text
				case "dateInForce":
					doc.DateInForce = text
				case "lastChangeInForce":
					doc.LastChangeInForce = text
				case "dateOfPublication":
					doc.DateOfPublication = text
				case "appliesTo":
					doc.AppliesTo = strings.TrimSpace(strings.TrimPrefix(text, "Gjelder for"))
				case "eeaReferences":
					doc.EEAReferences = text
					for _, m := range directivePattern.FindAllStringSubmatch(text, -1) {
						doc.EUDirectiveIDs = appendUniq(doc.EUDirectiveIDs, strings.ToUpper(m[1]))
					}
				case "ministry":
					for _, li := range findAll(n, "li") {
						mn := strings.TrimSpace(nodeText(li))
						if mn != "" && doc.Ministry == "" {
							doc.Ministry = mn
						}
					}
				case "legalArea":
					for _, a := range findAll(n, "a") {
						href := attr(a, "href")
						areaName := strings.TrimSpace(nodeText(a))
						if areaName == "" {
							continue
						}
						if strings.Contains(href, ".") {
							doc.AllLegalSubAreas = appendUniq(doc.AllLegalSubAreas, areaName)
							if doc.LegalSubArea == "" {
								doc.LegalSubArea = areaName
							}
						} else {
							doc.AllLegalAreas = appendUniq(doc.AllLegalAreas, areaName)
							if doc.LegalArea == "" {
								doc.LegalArea = areaName
							}
						}
					}
				case "lastChangedBy":
					if a := findFirst(n, "a"); a != nil {
						ref := strings.TrimPrefix(strings.TrimSpace(attr(a, "href")), "/")
						if sp := strings.Index(ref, " fra "); sp > 0 {
							ref = ref[:sp]
						}
						doc.LastChangedByRef = ref
					}
				case "changesToDocuments":
					for _, a := range findAll(n, "a") {
						ref := stripAnchor(strings.TrimPrefix(strings.TrimSpace(attr(a, "href")), "/"))
						if refPattern.MatchString(ref) {
							doc.AmendsRefs = appendUniq(doc.AmendsRefs, ref)
						}
					}
				case "miscInformation":
					for _, a := range findAll(n, "a") {
						ref := stripAnchor(strings.TrimPrefix(strings.TrimSpace(attr(a, "href")), "/"))
						if refPattern.MatchString(ref) {
							doc.SeeAlsoRefs = appendUniq(doc.SeeAlsoRefs, ref)
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkMeta(c)
		}
	}
	walkMeta(root)

	// Cross-references from body
	refSet := make(map[string]bool)
	var walkRefs func(*html.Node)
	walkRefs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := stripAnchor(strings.TrimPrefix(attr(n, "href"), "/"))
			if refPattern.MatchString(href) && href != doc.RefID {
				refSet[href] = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkRefs(c)
		}
	}

	var findMain func(*html.Node) *html.Node
	findMain = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "main" && attr(n, "id") == "dokument" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if r := findMain(c); r != nil {
				return r
			}
		}
		return nil
	}
	if main := findMain(root); main != nil {
		walkRefs(main)
		doc.Content, doc.Paragraphs, doc.EUBodyRefs = extractBody(main)
	}
	for ref := range refSet {
		doc.References = append(doc.References, ref)
	}
	sort.Strings(doc.References)

	if doc.RefID == "" {
		return nil
	}
	return doc
}

// ─── Archive download ─────────────────────────────────────────────────────────

func downloadAndCache(rawURL, cacheDir string) (string, error) {
	os.MkdirAll(cacheDir, 0755) //nolint:errcheck
	filename := rawURL[strings.LastIndex(rawURL, "/")+1:]
	localPath := filepath.Join(cacheDir, filename)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Printf("  Downloading %s ...", rawURL)
		resp, err := http.Get(rawURL) //nolint:noctx
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
		}
		f, err := os.Create(localPath)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(f, resp.Body)
		f.Close()
		if err != nil {
			return "", err
		}
		log.Printf("  Saved %s", localPath)
	} else {
		log.Printf("  Using cached %s", filename)
	}
	return localPath, nil
}

func loadDataset(dataURL, docType string, limit int, cacheDir string) ([]LovDoc, sourceArchive, error) {
	path, err := downloadAndCache(dataURL, cacheDir)
	if err != nil {
		return nil, sourceArchive{}, err
	}

	// Record the source archive fingerprint (name/sha256/size) for the manifest.
	// mtime is logged only, never written to the manifest, so re-dumps stay
	// byte-identical.
	archName := filepath.Base(path)
	archSHA, archSize, err := hashFile(path)
	if err != nil {
		return nil, sourceArchive{}, fmt.Errorf("hash archive %s: %w", archName, err)
	}
	arch := sourceArchive{Name: archName, SHA256: archSHA, Size: archSize}
	if fi, statErr := os.Stat(path); statErr == nil {
		log.Printf("  Archive %s: sha256=%s size=%d mtime=%s", archName, archSHA, archSize, fi.ModTime().UTC().Format(time.RFC3339))
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, sourceArchive{}, err
	}
	defer f.Close()

	bzr := bzip2.NewReader(f)
	tr := tar.NewReader(bzr)

	var docs []LovDoc
	var mu sync.Mutex
	done := false

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, sourceArchive{}, fmt.Errorf("tar read: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".xml") {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			continue
		}

		mu.Lock()
		if done {
			mu.Unlock()
			continue
		}
		mu.Unlock()

		d := parseDocument(content, docType)
		if d == nil {
			continue
		}

		mu.Lock()
		if limit > 0 && len(docs) >= limit {
			done = true
			mu.Unlock()
			continue
		}
		docs = append(docs, *d)
		mu.Unlock()
	}
	return docs, arch, nil
}

// ─── EUR-Lex fetching ─────────────────────────────────────────────────────────

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	},
}

func directiveToCELEX(id string) string {
	parts := strings.Split(strings.ToUpper(id), "/")
	if len(parts) < 3 {
		return ""
	}
	num, err := strconv.Atoi(parts[1])
	if err != nil {
		return ""
	}
	return fmt.Sprintf("3%sL%04d", parts[0], num)
}

func fetchEURLex(ctx context.Context, directiveID string) (*EUDirective, error) {
	celexL := directiveToCELEX(directiveID)
	celexR := strings.Replace(celexL, "L", "R", 1)
	var dir *EUDirective
	for _, celex := range []string{celexL, celexR} {
		if celex == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "GET", eurLexBase+celex, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NorwegianLawSeeder/1.0)")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		dir = parseEURLex(directiveID, celex, body)
		break
	}
	if dir == nil {
		dir = &EUDirective{DirectiveID: directiveID, CelexID: celexL}
	}

	// CELLAR full-text
	if celexL != "" {
		uuid := cellarUUID(ctx, celexL)
		if uuid == "" && celexR != "" {
			uuid = cellarUUID(ctx, celexR)
		}
		if uuid != "" {
			if content, fmt := fetchCellarContent(ctx, uuid); len(content) > 0 {
				switch fmt {
				case "legacy":
					dir.Content = extractEUBodyLegacy(content)
				case "formex":
					dir.Content = extractEUBodyFormex(content)
				default:
					dir.Content = extractEUBody(content)
				}
			}
		}
	}

	return dir, nil
}

func parseEURLex(directiveID, celex string, content []byte) *EUDirective {
	dir := &EUDirective{DirectiveID: directiveID, CelexID: celex}
	text := string(content)

	if m := regexp.MustCompile(`name="WT\.z_docTitle"\s+content="([^"]+)"`).FindStringSubmatch(text); m != nil {
		dir.FullTitle = m[1]
	}
	for _, m := range eurovocPattern.FindAllStringSubmatch(text, -1) {
		dir.EuroVocIDs = appendUniq(dir.EuroVocIDs, m[1])
	}

	root, err := html.Parse(strings.NewReader(text))
	if err == nil {
		var label string
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch n.Data {
				case "th", "dt":
					label = strings.TrimSpace(nodeText(n))
				case "td", "dd":
					val := strings.Join(strings.Fields(strings.TrimSpace(nodeText(n))), " ")
					switch {
					case strings.Contains(label, "Date of document") && dir.DateOfDocument == "":
						dir.DateOfDocument = val
					case strings.Contains(label, "Date of effect") && dir.DateOfEffect == "":
						if idx := strings.Index(val, ";"); idx > 0 {
							dir.DateOfEffect = strings.TrimSpace(val[:idx])
						} else {
							dir.DateOfEffect = val
						}
					case strings.Contains(label, "Form") && dir.Form == "":
						dir.Form = val
					case strings.Contains(label, "Author") && dir.Author == "":
						dir.Author = val
					case strings.Contains(label, "Responsible body") && dir.ResponsibleDG == "":
						dir.ResponsibleDG = val
					case strings.Contains(label, "Subject matter") && dir.SubjectMatter == "":
						dir.SubjectMatter = val
					case strings.Contains(label, "Directory code") && dir.DirectoryCode == "":
						dir.DirectoryCode = val
					case strings.Contains(label, "Legal basis") && dir.LegalBasis == "":
						dir.LegalBasis = val
					case strings.Contains(label, "Procedure number") && dir.ProcedureNum == "":
						dir.ProcedureNum = val
					case strings.Contains(label, "Instruments cited"):
						for _, m := range celexPattern.FindAllString(val, -1) {
							dir.CitedCELEX = appendUniq(dir.CitedCELEX, m)
						}
					case strings.Contains(label, "Modified by"):
						for _, m := range celexPattern.FindAllString(val, -1) {
							dir.ModifiedByCELEX = appendUniq(dir.ModifiedByCELEX, m)
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}

	if m := regexp.MustCompile(`OJ [A-Z]+ \d+[^<\n]{3,40}`).FindString(text); m != "" {
		dir.OJReference = strings.TrimSpace(m)
	}
	if dir.FullTitle != "" {
		short := dir.FullTitle
		if idx := strings.Index(short, "("); idx > 20 {
			short = strings.TrimSpace(short[:idx])
		}
		if len(short) > 120 {
			short = short[:120]
		}
		dir.ShortTitle = short
	}
	return dir
}

func cellarUUID(ctx context.Context, celexID string) string {
	query := fmt.Sprintf(`PREFIX cdm: <http://publications.europa.eu/ontology/cdm#>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
SELECT ?work WHERE {
  ?work cdm:resource_legal_id_celex "%s"^^xsd:string .
} LIMIT 1`, celexID)

	form := url.Values{"query": {query}}
	req, err := http.NewRequestWithContext(ctx, "POST", cellarSPQL, strings.NewReader(form.Encode()))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Results struct {
			Bindings []struct {
				Work struct{ Value string } `json:"work"`
			} `json:"bindings"`
		} `json:"results"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil || len(result.Results.Bindings) == 0 {
		return ""
	}
	uri := result.Results.Bindings[0].Work.Value
	if idx := strings.LastIndex(uri, "/"); idx >= 0 {
		return uri[idx+1:]
	}
	return ""
}

func fetchCellarContent(ctx context.Context, cellarUUID string) ([]byte, string) {
	type candidate struct {
		seq    int
		body   []byte
		format string
	}
	var first *candidate
	for seq := 3; seq <= 26; seq++ {
		seqStr := fmt.Sprintf("%02d", seq)
		reqURL := fmt.Sprintf("%s%s.0001.%s/DOC_1", cellarBase, cellarUUID, seqStr)
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			break
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			break
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(body) < 1000 {
			if resp.StatusCode == http.StatusNotFound && seq > 5 {
				break
			}
			continue
		}
		prefix := body[:minInt(500, len(body))]
		if bytes.Contains(prefix, []byte("<ACT")) && bytes.Contains(body, []byte("<ARTICLE")) {
			c := &candidate{seq: seq, body: body, format: "formex"}
			if first == nil {
				first = c
			}
			continue
		}
		if bytes.Contains(prefix, []byte("<html")) || bytes.Contains(prefix, []byte("<?xml")) {
			if !bytes.Contains(body, []byte("eli-subdivision")) &&
				!bytes.Contains(body, []byte("oj-ti-art")) {
				continue
			}
			lang := ""
			if idx := bytes.Index(body, []byte(`class="oj-hd-lg">`)); idx >= 0 {
				rest := body[idx+len(`class="oj-hd-lg">`):]
				if end := bytes.Index(rest, []byte("<")); end > 0 && end <= 5 {
					lang = strings.TrimSpace(string(rest[:end]))
				}
			}
			c := &candidate{seq: seq, body: body, format: "oj"}
			if first == nil {
				first = c
			}
			if lang == "EN" {
				return body, "oj"
			}
		}
	}
	if first != nil {
		return first.body, first.format
	}

	reqURL := fmt.Sprintf("%s%s.0001.01/DOC_1", cellarBase, cellarUUID)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err == nil {
		resp, err := httpClient.Do(req)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && len(body) > 1000 &&
				bytes.Contains(body, []byte("<TXT_TE>")) {
				return body, "legacy"
			}
		}
	}
	return nil, ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── EU body extraction ───────────────────────────────────────────────────────

func extractEUBody(xhtml []byte) string {
	root, err := html.Parse(bytes.NewReader(xhtml))
	if err != nil {
		return ""
	}

	var sb strings.Builder

	var extractText func(*html.Node) string
	extractText = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var s strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s.WriteString(extractText(c))
		}
		return s.String()
	}

	getAttr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}

	hasClass := func(n *html.Node, cls string) bool {
		return strings.Contains(getAttr(n, "class"), cls)
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		cls := getAttr(n, "class")
		id := getAttr(n, "id")

		switch {
		case hasClass(n, "oj-hd-date") || hasClass(n, "oj-hd-lg") ||
			hasClass(n, "oj-hd-ti") || hasClass(n, "oj-hd-oj"):
			return
		case hasClass(n, "eli-main-title"):
			return
		case id == "pbl_1":
			return
		case n.Data == "div" && hasClass(n, "eli-subdivision") && strings.HasPrefix(id, "art_"):
			var artNum, artTitle string
			var findArticleHeaders func(*html.Node)
			findArticleHeaders = func(inner *html.Node) {
				if inner.Type == html.ElementNode {
					ic := getAttr(inner, "class")
					if strings.Contains(ic, "oj-ti-art") {
						artNum = strings.TrimSpace(extractText(inner))
					} else if strings.Contains(ic, "oj-sti-art") {
						artTitle = strings.TrimSpace(extractText(inner))
					}
				}
				for c := inner.FirstChild; c != nil; c = c.NextSibling {
					findArticleHeaders(c)
				}
			}
			findArticleHeaders(n)
			header := artNum
			if artTitle != "" {
				header += " — " + artTitle
			}
			if header != "" {
				sb.WriteString("## " + header + "\n\n")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		case n.Data == "div" && hasClass(n, "eli-subdivision") && strings.HasPrefix(id, "rct_"):
			text := strings.Join(strings.Fields(strings.TrimSpace(extractText(n))), " ")
			if text != "" {
				num := strings.TrimPrefix(id, "rct_")
				sb.WriteString("(" + num + ") " + text + "\n\n")
			}
			return
		case n.Data == "div" && hasClass(n, "eli-subdivision") && strings.HasPrefix(id, "cit_"):
			return
		case hasClass(n, "oj-ti-art") || hasClass(n, "oj-sti-art"):
			return
		case n.Data == "p" && hasClass(n, "oj-normal"):
			text := strings.Join(strings.Fields(strings.TrimSpace(extractText(n))), " ")
			if text != "" {
				sb.WriteString(text + "\n\n")
			}
			return
		case hasClass(n, "oj-signatory") || hasClass(n, "oj-final"):
			return
		case hasClass(n, "oj-note"):
			return
		default:
			if n.Data == "table" && !strings.Contains(cls, "oj-hd") {
				var tableText func(*html.Node)
				tableText = func(inner *html.Node) {
					if inner.Type == html.ElementNode && inner.Data == "td" {
						text := strings.Join(strings.Fields(strings.TrimSpace(extractText(inner))), " ")
						if text != "" && len(text) > 10 {
							sb.WriteString(text + "\n\n")
						}
					}
					for c := inner.FirstChild; c != nil; c = c.NextSibling {
						tableText(c)
					}
				}
				tableText(n)
				return
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(root)
	return strings.TrimSpace(sb.String())
}

func extractEUBodyLegacy(htmlBody []byte) string {
	start := bytes.Index(htmlBody, []byte("<TXT_TE>"))
	if start < 0 {
		start = bytes.Index(htmlBody, []byte(`<div id="TexteOnly">`))
		if start < 0 {
			return ""
		}
	}
	end := bytes.Index(htmlBody[start:], []byte("</TXT_TE>"))
	if end < 0 {
		end = bytes.Index(htmlBody[start:], []byte("</div>"))
	}
	if end < 0 {
		end = len(htmlBody) - start
	}
	chunk := htmlBody[start : start+end]

	root, err := html.Parse(bytes.NewReader(chunk))
	if err != nil {
		return ""
	}

	var sb strings.Builder
	var extractText func(*html.Node) string
	extractText = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var s strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s.WriteString(extractText(c))
		}
		return s.String()
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p":
				txt := strings.Join(strings.Fields(extractText(n)), " ")
				if txt != "" {
					sb.WriteString(txt)
					sb.WriteString("\n\n")
				}
				return
			case "br":
				sb.WriteString("\n")
				return
			case "h1", "h2", "h3":
				txt := strings.Join(strings.Fields(extractText(n)), " ")
				if txt != "" {
					sb.WriteString("## ")
					sb.WriteString(txt)
					sb.WriteString("\n\n")
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return strings.TrimSpace(sb.String())
}

func extractEUBodyFormex(xmlBody []byte) string {
	text := string(xmlBody)
	var sb strings.Builder

	stripXML := func(s string) string {
		s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
		return strings.Join(strings.Fields(s), " ")
	}

	artRe := regexp.MustCompile(`(?s)<ARTICLE[^>]*>(.*?)</ARTICLE>`)
	tiArtRe := regexp.MustCompile(`(?s)<TI\.ART>(.*?)</TI\.ART>`)
	stiArtRe := regexp.MustCompile(`(?s)<STI\.ART>(.*?)</STI\.ART>`)
	alineaRe := regexp.MustCompile(`(?s)<(?:ALINEA|P)>(.*?)</(?:ALINEA|P)>`)

	for _, artMatch := range artRe.FindAllStringSubmatch(text, -1) {
		artBody := artMatch[1]
		var artNum, artTitle string
		if m := tiArtRe.FindStringSubmatch(artBody); m != nil {
			artNum = stripXML(m[1])
		}
		if m := stiArtRe.FindStringSubmatch(artBody); m != nil {
			artTitle = stripXML(m[1])
		}
		header := artNum
		if header == "" {
			header = "Article"
		}
		if artTitle != "" {
			header += " — " + artTitle
		}
		sb.WriteString("## ")
		sb.WriteString(header)
		sb.WriteString("\n\n")

		for _, alineaMatch := range alineaRe.FindAllStringSubmatch(artBody, -1) {
			t := stripXML(alineaMatch[1])
			if t != "" && len(t) > 5 {
				sb.WriteString(t)
				sb.WriteString("\n\n")
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// ─── EuroVoc SPARQL ───────────────────────────────────────────────────────────

func fetchEuroVoc(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	vals := make([]string, len(ids))
	for i, id := range ids {
		vals[i] = fmt.Sprintf("<http://eurovoc.europa.eu/%s>", id)
	}
	query := fmt.Sprintf(`PREFIX skos: <http://www.w3.org/2004/02/skos/core#>
SELECT ?id ?label WHERE {
  VALUES ?id { %s }
  ?id skos:prefLabel ?label .
  FILTER(LANG(?label) = "en")
}`, strings.Join(vals, " "))

	form := url.Values{"query": {query}}
	req, err := http.NewRequestWithContext(ctx, "POST", eurovocSPQL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Results struct {
			Bindings []struct {
				ID    struct{ Value string } `json:"id"`
				Label struct{ Value string } `json:"label"`
			} `json:"bindings"`
		} `json:"results"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	labels := make(map[string]string)
	for _, b := range result.Results.Bindings {
		uri := b.ID.Value
		if idx := strings.LastIndex(uri, "/"); idx >= 0 {
			labels[uri[idx+1:]] = b.Label.Value
		}
	}
	return labels, nil
}

// ─── EU data orchestration ────────────────────────────────────────────────────

func fetchAllEUData(ctx context.Context, docs []LovDoc, euLimit int) ([]*EUDirective, []*EuroVocConcept) {
	allIDs := make(map[string]bool)
	for _, d := range docs {
		for _, did := range d.EUDirectiveIDs {
			allIDs[did] = true
		}
	}
	ids := make([]string, 0, len(allIDs))
	for id := range allIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if euLimit > 0 && len(ids) > euLimit {
		ids = ids[:euLimit]
	}
	log.Printf("  Fetching %d EU directives from EUR-Lex (max 5 concurrent) ...", len(ids))

	sem := make(chan struct{}, 5)
	var mu sync.Mutex
	var directives []*EUDirective
	var wg sync.WaitGroup

	for _, id := range ids {
		sem <- struct{}{}
		wg.Add(1)
		go func(did string) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			dir, err := fetchEURLex(ctx, did)
			if err != nil {
				dir = &EUDirective{DirectiveID: did, CelexID: directiveToCELEX(did)}
			}
			mu.Lock()
			directives = append(directives, dir)
			mu.Unlock()
			time.Sleep(200 * time.Millisecond)
		}(id)
	}
	wg.Wait()
	sortDirectives(directives)

	evIDs := make(map[string]bool)
	for _, dir := range directives {
		for _, id := range dir.EuroVocIDs {
			evIDs[id] = true
		}
	}
	evList := make([]string, 0, len(evIDs))
	for id := range evIDs {
		evList = append(evList, id)
	}
	sort.Strings(evList)
	log.Printf("  Fetching %d EuroVoc concept labels via SPARQL ...", len(evList))

	evLabels := make(map[string]string)
	for i := 0; i < len(evList); i += 50 {
		end := i + 50
		if end > len(evList) {
			end = len(evList)
		}
		labels, err := fetchEuroVoc(ctx, evList[i:end])
		if err != nil {
			log.Printf("  [EuroVoc] batch %d-%d error: %v", i, end, err)
			continue
		}
		for id, label := range labels {
			evLabels[id] = label
		}
		time.Sleep(100 * time.Millisecond)
	}

	var concepts []*EuroVocConcept
	for id, label := range evLabels {
		concepts = append(concepts, &EuroVocConcept{ID: id, LabelEN: label})
	}
	sortConcepts(concepts)
	log.Printf("  Fetched %d/%d EuroVoc labels", len(concepts), len(evList))
	return directives, concepts
}

// sortDirectives orders directives by DirectiveID (falling back to CelexID when
// the DirectiveID is empty) so the EU seed is emitted deterministically.
func sortDirectives(dirs []*EUDirective) {
	sort.SliceStable(dirs, func(i, j int) bool {
		ki := dirs[i].DirectiveID
		if ki == "" {
			ki = dirs[i].CelexID
		}
		kj := dirs[j].DirectiveID
		if kj == "" {
			kj = dirs[j].CelexID
		}
		return ki < kj
	})
}

// sortConcepts orders concepts by ID so the EuroVoc seed is deterministic.
func sortConcepts(concepts []*EuroVocConcept) {
	sort.SliceStable(concepts, func(i, j int) bool {
		return concepts[i].ID < concepts[j].ID
	})
}

// ─── Seed records (shared between upload + dump) ──────────────────────────────

type seedObjectRecord struct {
	Type       string
	Key        string
	Properties map[string]any
}

type seedRelationshipRecord struct {
	Type       string
	SrcKey     string
	DstKey     string
	Properties map[string]any
}

// dedupeSeedObjects drops records that reuse an already-emitted object key.
// Lovdata publishes some acts in several language versions sharing one RefID
// (and thus one key), which would otherwise produce duplicate objects and a
// hard 409 conflict on install. First occurrence wins.
func dedupeSeedObjects(in []seedObjectRecord) []seedObjectRecord {
	seen := make(map[string]bool, len(in))
	out := make([]seedObjectRecord, 0, len(in))
	for _, r := range in {
		if seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		out = append(out, r)
	}
	return out
}

// dedupeSeedRelationships drops duplicate (type, srcKey, dstKey) edges, which
// arise from the same language-variant duplication fixed in dedupeSeedObjects.
func dedupeSeedRelationships(in []seedRelationshipRecord) []seedRelationshipRecord {
	seen := make(map[string]bool, len(in))
	out := make([]seedRelationshipRecord, 0, len(in))
	for _, r := range in {
		k := r.Type + "\x00" + r.SrcKey + "\x00" + r.DstKey
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// seedDateProps lists the object types whose properties are date-typed in the
// blueprint schema pack, and the property names that must be emitted as valid
// YYYY-MM-DD dates (never an empty/invalid string, which the server rejects
// with a 400 for the whole object). date_of_publication is date-only even
// though Lovdata carries a clock time (e.g. "2024-07-09 14:55") — the schema
// types it as a bare date, so the clock time is dropped.
var seedDateProps = map[string][]string{
	"Law":         {"date_in_force", "last_change_in_force", "date_of_publication"},
	"Regulation":  {"date_in_force", "last_change_in_force", "date_of_publication"},
	"EUDirective": {"date_of_document", "date_of_effect"},
}

// seedDateRawProps maps a date/datetime property to the companion raw-text field
// where an unparseable non-empty value is preserved instead of being dropped.
var seedDateRawProps = map[string]string{
	"date_in_force":       "date_in_force_raw",
	"date_of_publication": "date_of_publication_raw",
}

// parseDateValue parses s with every accepted date/datetime layout and applies
// the "year > 2050" typo guard (values like "2051-01-01" are treated as
// deferred/typo, per the reference parser). It returns the parsed time, or
// ok=false if s is empty, malformed, or carries an implausible year.
func parseDateValue(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"01/02/2006",
		"02-01-2006",
		"02.01.2006",
		"02.01.2006 15:04",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			if t.Year() > 2050 {
				return time.Time{}, false
			}
			return t, true
		}
	}
	return time.Time{}, false
}

// yearFromLooseDate extracts a plausible year from a date-like string that may
// be free text (e.g. "Inntektsåret 1998", "Kongen bestemmer, 2004-01-01,
// 2005-01-01"). It first tries the strict s[:4] prefix, then scans for 4-digit
// runs and returns the first one in 1000..2050. The range is deliberately not
// limited to (19|20) — the corpus contains pre-1900 acts (e.g. 1814). Returns
// ok=false when no plausible year is found.
func yearFromLooseDate(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if len(s) >= 4 {
		if yr, err := strconv.Atoi(s[:4]); err == nil {
			return yr, true
		}
	}
	for i := 0; i+4 <= len(s); i++ {
		run := s[i : i+4]
		isDigits := true
		for j := 0; j < 4; j++ {
			if run[j] < '0' || run[j] > '9' {
				isDigits = false
				break
			}
		}
		if !isDigits {
			continue
		}
		yr, err := strconv.Atoi(run)
		if err != nil {
			continue
		}
		if yr >= 1000 && yr <= 2050 {
			return yr, true
		}
	}
	return 0, false
}

// normalizeDateValue parses a property value and returns the normalized
// YYYY-MM-DD form, or ok=false if the value is empty, non-string, or not a
// parseable date.
func normalizeDateValue(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	t, ok := parseDateValue(s)
	if !ok {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

// normalizeDateProps normalizes the date-typed properties of an object record
// in place: parseable values are rewritten to their canonical YYYY-MM-DD form,
// unparseable/empty ones are dropped, and an unparseable non-empty value is
// preserved in its raw companion field (date_in_force_raw /
// date_of_publication_raw) so the information is never silently lost.
func normalizeDateProps(objType string, props map[string]any, normCounts map[string]int, rawMoved *int) {
	normalizeFields := func(keys []string, normFn func(any) (string, bool)) {
		for _, key := range keys {
			raw, present := props[key]
			if !present {
				continue
			}
			if norm, ok := normFn(raw); ok {
				props[key] = norm
				normCounts[key]++
				continue
			}
			delete(props, key)
			if rawKey := seedDateRawProps[key]; rawKey != "" {
				if s, isStr := raw.(string); isStr && strings.TrimSpace(s) != "" {
					if _, exists := props[rawKey]; !exists {
						props[rawKey] = s
						(*rawMoved)++
					}
				}
			} else if s, isStr := raw.(string); isStr && strings.TrimSpace(s) != "" {
				// No raw companion is configured for this key, so a non-empty
				// unparseable value would vanish with no trace — warn loudly.
				log.Printf("  [date] dropping unparseable %s.%s=%q (no raw companion configured)", objType, key, s)
			}
		}
	}
	normalizeFields(seedDateProps[objType], normalizeDateValue)
}

// ─── Object ingestion (Phase 1) ───────────────────────────────────────────────

// buildSeedObjectRecords builds the full ordered list of objects as key/property
// records, independent of any server. Both the upload path (ingestObjects) and
// the dump path consume these records, so the two can never diverge.
func buildSeedObjectRecords(docs []LovDoc, directives []*EUDirective, concepts []*EuroVocConcept) []seedObjectRecord {
	var records []seedObjectRecord

	normCounts := make(map[string]int)
	rawMoved := 0

	ministries := make(map[string]bool)
	legalAreas := make(map[string]bool)
	legalSubAreas := make(map[string]string)

	for _, d := range docs {
		if d.Ministry != "" {
			ministries[d.Ministry] = true
		}
		for _, a := range d.AllLegalAreas {
			legalAreas[a] = true
		}
		for _, sa := range d.AllLegalSubAreas {
			if _, exists := legalSubAreas[sa]; !exists {
				legalSubAreas[sa] = d.LegalArea
			}
		}
	}

	for _, m := range sortedMapKeys(ministries) {
		records = append(records, seedObjectRecord{
			Type: "Ministry", Key: "ministry_" + m,
			Properties: map[string]any{"name": m},
		})
	}
	for _, a := range sortedMapKeys(legalAreas) {
		records = append(records, seedObjectRecord{
			Type: "LegalArea", Key: "area_" + a,
			Properties: map[string]any{"name": a},
		})
	}
	for _, sa := range sortedMapKeys(legalSubAreas) {
		records = append(records, seedObjectRecord{
			Type: "LegalArea", Key: "subarea_" + sa,
			Properties: map[string]any{"name": sa, "parent_area": legalSubAreas[sa]},
		})
	}

	for _, d := range docs {
		k := d.RefID
		props := map[string]any{
			"name":   d.Title,
			"title":  d.Title,
			"ref_id": d.RefID,
			"doc_id": d.DocID,
		}
		if d.ShortTitle != "" {
			props["short_title"] = d.ShortTitle
		}
		if d.LegacyID != "" {
			props["legacy_id"] = d.LegacyID
		}
		if d.Language != "" {
			props["language"] = d.Language
		}
		if d.DateInForce != "" {
			props["date_in_force"] = d.DateInForce
			if yr, ok := yearFromLooseDate(d.DateInForce); ok {
				props["year_in_force"] = yr
				props["decade_in_force"] = fmt.Sprintf("%ds", (yr/10)*10)
			}
		}
		if d.LastChangeInForce != "" {
			props["last_change_in_force"] = d.LastChangeInForce
		}
		if d.DateOfPublication != "" {
			props["date_of_publication"] = d.DateOfPublication
		}
		if d.AppliesTo != "" {
			props["applies_to"] = d.AppliesTo
		}
		if d.EEAReferences != "" {
			props["eea_references"] = d.EEAReferences
		}
		if d.Content != "" {
			props["content"] = d.Content
		}
		normalizeDateProps(d.DocType, props, normCounts, &rawMoved)
		records = append(records, seedObjectRecord{
			Type: d.DocType, Key: k, Properties: props,
		})

		for _, p := range d.Paragraphs {
			if p.Content == "" || p.SectionID == "" {
				continue
			}
			pKey := d.RefID + "#" + p.SectionID
			pName := p.ParagraphNum
			if p.Title != "" {
				pName += " " + p.Title
			}
			if strings.TrimSpace(pName) == "" {
				// Top-level legalP chunks carry no § number/title; fall back to
				// the section id so the object never has an empty name.
				pName = p.SectionID
			}
			pProps := map[string]any{
				"name":          pName,
				"content":       p.Content,
				"section_id":    p.SectionID,
				"paragraph_num": p.ParagraphNum,
				"law_ref_id":    d.RefID,
				"position":      p.Position,
			}
			if p.ChapterID != "" {
				pProps["chapter_id"] = p.ChapterID
			}
			if p.Title != "" {
				pProps["title"] = p.Title
			}
			records = append(records, seedObjectRecord{
				Type: "LegalParagraph", Key: pKey, Properties: pProps,
			})
		}
	}

	for _, dir := range directives {
		k := "eu_" + dir.CelexID
		name := dir.ShortTitle
		if name == "" {
			name = dir.CelexID
		}
		props := map[string]any{
			"name":         name,
			"celex_id":     dir.CelexID,
			"directive_id": dir.DirectiveID,
		}
		if dir.FullTitle != "" {
			props["full_title"] = dir.FullTitle
		}
		if dir.Form != "" {
			props["form"] = dir.Form
		}
		if dir.DateOfDocument != "" {
			props["date_of_document"] = dir.DateOfDocument
		}
		if dir.DateOfEffect != "" {
			props["date_of_effect"] = dir.DateOfEffect
		}
		if dir.Author != "" {
			props["author"] = dir.Author
		}
		if dir.SubjectMatter != "" {
			props["subject_matter"] = dir.SubjectMatter
		}
		if dir.OJReference != "" {
			props["oj_reference"] = dir.OJReference
		}
		if dir.Content != "" {
			props["content"] = dir.Content
		}
		normalizeDateProps("EUDirective", props, normCounts, &rawMoved)
		records = append(records, seedObjectRecord{
			Type: "EUDirective", Key: k, Properties: props,
		})
	}

	for _, ev := range concepts {
		records = append(records, seedObjectRecord{
			Type: "EuroVocConcept", Key: "eurovoc_" + ev.ID,
			Properties: map[string]any{
				"name":       ev.LabelEN,
				"eurovoc_id": ev.ID,
				"label_en":   ev.LabelEN,
			},
		})
	}

	normParts := make([]string, 0, len(normCounts))
	for _, k := range sortedMapKeys(normCounts) {
		normParts = append(normParts, fmt.Sprintf("%s=%d", k, normCounts[k]))
	}
	log.Printf("  Date normalization: normalized [%s], raw-preserved: %d", strings.Join(normParts, " "), rawMoved)

	return dedupeSeedObjects(records)
}

func ingestObjects(ctx context.Context, client *graph.Client, docs []LovDoc, directives []*EUDirective, concepts []*EuroVocConcept, batchSz, nWorkers int) map[string]string {
	records := buildSeedObjectRecords(docs, directives, concepts)
	items := make([]graph.CreateObjectRequest, 0, len(records))
	for _, r := range records {
		items = append(items, graph.CreateObjectRequest{
			Type: r.Type, Key: strPtr(r.Key), Properties: r.Properties,
		})
	}

	log.Printf("  Uploading %d objects in batches of %d with %d workers ...", len(items), batchSz, nWorkers)
	return bulkUploadObjects(ctx, client, items, batchSz, nWorkers)
}

// ─── Relationship ingestion (Phase 2) ─────────────────────────────────────────

// buildSeedRelationshipRecords builds the full ordered list of relationships as
// key-based records, independent of any server. `objectKeys` is the set of keys
// emitted by buildSeedObjectRecords; it plays the role of `knownRefs` for the
// SEE_ALSO/REFERENCES filter (semantically identical, since those refs are
// always document RefIDs) and lets the dump path verify endpoint existence.
func buildSeedRelationshipRecords(docs []LovDoc, directives []*EUDirective, objectKeys map[string]bool) []seedRelationshipRecord {
	var records []seedRelationshipRecord

	dirByID := make(map[string]*EUDirective, len(directives))
	for _, dir := range directives {
		dirByID[dir.DirectiveID] = dir
		dirByID[strings.Replace(dir.DirectiveID, "EF", "EU", 1)] = dir
	}

	for _, d := range docs {
		if d.Ministry != "" {
			records = append(records, seedRelationshipRecord{
				Type: "ADMINISTERED_BY", SrcKey: d.RefID, DstKey: "ministry_" + d.Ministry,
				Properties: map[string]any{},
			})
		}
		for _, area := range d.AllLegalAreas {
			records = append(records, seedRelationshipRecord{
				Type: "IN_LEGAL_AREA", SrcKey: d.RefID, DstKey: "area_" + area,
				Properties: map[string]any{},
			})
		}
		for _, sub := range d.AllLegalSubAreas {
			records = append(records, seedRelationshipRecord{
				Type: "IN_LEGAL_AREA", SrcKey: d.RefID, DstKey: "subarea_" + sub,
				Properties: map[string]any{"level": "sub"},
			})
		}
		for _, ref := range d.AmendsRefs {
			records = append(records, seedRelationshipRecord{
				Type: "AMENDS", SrcKey: d.RefID, DstKey: ref,
				Properties: map[string]any{},
			})
		}
		for _, ref := range d.SeeAlsoRefs {
			if objectKeys[ref] {
				records = append(records, seedRelationshipRecord{
					Type: "SEE_ALSO", SrcKey: d.RefID, DstKey: ref,
					Properties: map[string]any{},
				})
			}
		}
		for _, ref := range d.References {
			if objectKeys[ref] {
				records = append(records, seedRelationshipRecord{
					Type: "REFERENCES", SrcKey: d.RefID, DstKey: ref,
					Properties: map[string]any{},
				})
			}
		}
		for _, did := range d.EUDirectiveIDs {
			if dir := dirByID[did]; dir != nil {
				records = append(records, seedRelationshipRecord{
					Type: "IMPLEMENTS_EEA", SrcKey: d.RefID, DstKey: "eu_" + dir.CelexID,
					Properties: map[string]any{"directive_id": did},
				})
			}
		}
		for _, p := range d.Paragraphs {
			if p.Content == "" || p.SectionID == "" {
				continue
			}
			pKey := d.RefID + "#" + p.SectionID
			records = append(records, seedRelationshipRecord{
				Type: "HAS_PARAGRAPH", SrcKey: d.RefID, DstKey: pKey,
				Properties: map[string]any{"position": p.Position},
			})
		}
		seenEU := make(map[string]bool)
		for _, euCode := range d.EUBodyRefs {
			euKey := "eu_" + euCode
			if seenEU[euKey] {
				continue
			}
			seenEU[euKey] = true
			records = append(records, seedRelationshipRecord{
				Type: "CITES_EU_LAW", SrcKey: d.RefID, DstKey: euKey,
				Properties: map[string]any{},
			})
		}
	}

	// AMENDED_BY: separate pass using LastChangedByRef
	for _, d := range docs {
		if d.LastChangedByRef != "" {
			records = append(records, seedRelationshipRecord{
				Type: "AMENDED_BY", SrcKey: d.RefID, DstKey: d.LastChangedByRef,
				Properties: map[string]any{"effective_date": d.LastChangeInForce},
			})
		}
	}

	// HAS_LANGUAGE_VARIANT
	docsByDocID := make(map[string][]LovDoc)
	for _, d := range docs {
		docsByDocID[d.DocID] = append(docsByDocID[d.DocID], d)
	}
	for _, group := range docsByDocID {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				records = append(records, seedRelationshipRecord{
					Type: "HAS_LANGUAGE_VARIANT", SrcKey: group[i].RefID, DstKey: group[j].RefID,
					Properties: map[string]any{
						"source_language": group[i].Language,
						"target_language": group[j].Language,
					},
				})
			}
		}
	}

	// EU chain relationships
	for _, dir := range directives {
		srcKey := "eu_" + dir.CelexID
		for _, cited := range dir.CitedCELEX {
			records = append(records, seedRelationshipRecord{
				Type: "EU_CITES", SrcKey: srcKey, DstKey: "eu_" + cited,
				Properties: map[string]any{},
			})
		}
		for _, mod := range dir.ModifiedByCELEX {
			records = append(records, seedRelationshipRecord{
				Type: "EU_MODIFIED_BY", SrcKey: srcKey, DstKey: "eu_" + mod,
				Properties: map[string]any{},
			})
		}
		for _, evID := range dir.EuroVocIDs {
			records = append(records, seedRelationshipRecord{
				Type: "HAS_EUROVOC_DESCRIPTOR", SrcKey: srcKey, DstKey: "eurovoc_" + evID,
				Properties: map[string]any{},
			})
		}
	}

	return dedupeSeedRelationships(records)
}

func ingestRelationships(ctx context.Context, client *graph.Client, docs []LovDoc, directives []*EUDirective, idMap map[string]string, batchSz, nWorkers int, stateDir string) (int64, int64) {
	objectKeys := make(map[string]bool, len(idMap))
	for k := range idMap {
		objectKeys[k] = true
	}
	records := buildSeedRelationshipRecords(docs, directives, objectKeys)

	var items []graph.CreateRelationshipRequest
	for _, r := range records {
		src, ok := idMap[r.SrcKey]
		if !ok {
			continue
		}
		dst, ok2 := idMap[r.DstKey]
		if !ok2 || dst == src {
			continue
		}
		items = append(items, graph.CreateRelationshipRequest{
			Type: r.Type, SrcID: src, DstID: dst, Properties: r.Properties,
		})
	}

	log.Printf("  Uploading %d relationships in batches of %d with %d workers ...", len(items), batchSz, nWorkers)
	return bulkUploadRelationships(ctx, client, items, batchSz, nWorkers, stateDir)
}

// ─── Bulk upload: objects ─────────────────────────────────────────────────────

func bulkUploadObjects(ctx context.Context, client *graph.Client, items []graph.CreateObjectRequest, batchSz, nWorkers int) map[string]string {
	type batchResult struct {
		batch []graph.CreateObjectRequest
		res   *graph.BulkCreateObjectsResponse
	}

	batches := make(chan []graph.CreateObjectRequest, nWorkers*2)
	results := make(chan batchResult, nWorkers*2)

	var wg sync.WaitGroup
	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batches {
				if ctx.Err() != nil {
					return
				}
				res, err := client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: batch})
				if err != nil {
					log.Printf("  [objects] batch error: %v", err)
					time.Sleep(500 * time.Millisecond)
					res, _ = client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: batch})
				}
				results <- batchResult{batch, res}
			}
		}()
	}

	go func() { wg.Wait(); close(results) }()

	go func() {
		for i := 0; i < len(items); i += batchSz {
			if ctx.Err() != nil {
				break
			}
			end := i + batchSz
			if end > len(items) {
				end = len(items)
			}
			batches <- items[i:end]
		}
		close(batches)
	}()

	idMap := make(map[string]string)
	var mu sync.Mutex
	var uploaded, conflicts, failed atomic.Int64

	type missingKey struct{ objType, key string }
	var missingKeys []missingKey
	var missingMu sync.Mutex

	for br := range results {
		if br.res != nil {
			mu.Lock()
			for idx, result := range br.res.Results {
				k := br.batch[idx].Key
				if k == nil {
					continue
				}
				if result.Object != nil {
					id := result.Object.EntityID
					if id == "" {
						id = result.Object.CanonicalID
					}
					if id == "" {
						id = result.Object.ID
					}
					idMap[*k] = id
					uploaded.Add(1)
				} else if result.Error != nil && strings.Contains(*result.Error, "conflict") {
					missingMu.Lock()
					missingKeys = append(missingKeys, missingKey{br.batch[idx].Type, *k})
					missingMu.Unlock()
					conflicts.Add(1)
				} else if result.Error != nil {
					log.Printf("  [objects] error key=%s: %s", *k, *result.Error)
					failed.Add(1)
				}
			}
			mu.Unlock()
		}
		n := uploaded.Load() + conflicts.Load()
		if n%20000 == 0 && n > 0 {
			log.Printf("  ...%d/%d objects processed (new=%d conflicts=%d)", n, len(items), uploaded.Load(), conflicts.Load())
		}
	}

	if len(missingKeys) > 0 {
		log.Printf("  Resolving %d conflicting keys by lookup...", len(missingKeys))
		sem := make(chan struct{}, nWorkers)
		var resolveWg sync.WaitGroup
		for _, mk := range missingKeys {
			sem <- struct{}{}
			resolveWg.Add(1)
			go func(objType, key string) {
				defer resolveWg.Done()
				defer func() { <-sem }()
				resp, err := client.ListObjects(ctx, &graph.ListObjectsOptions{Type: objType, Key: key, Limit: 1})
				if err == nil && resp != nil && len(resp.Items) > 0 {
					obj := resp.Items[0]
					id := obj.EntityID
					if id == "" {
						id = obj.CanonicalID
					}
					if id == "" {
						id = obj.ID
					}
					mu.Lock()
					idMap[key] = id
					mu.Unlock()
				}
			}(mk.objType, mk.key)
		}
		resolveWg.Wait()
		log.Printf("  Conflict resolution complete — idMap now has %d entries", len(idMap))
	}

	log.Printf("  Objects complete: %d new, %d conflicts resolved, %d errors, %d total mapped",
		uploaded.Load(), conflicts.Load(), failed.Load(), len(idMap))
	return idMap
}

// ─── Bulk upload: relationships ───────────────────────────────────────────────

func bulkUploadRelationships(ctx context.Context, client *graph.Client, items []graph.CreateRelationshipRequest, batchSz, nWorkers int, stateDir string) (int64, int64) {
	relsDone := loadRelsDone(stateDir)

	var batches [][]graph.CreateRelationshipRequest
	for i := 0; i < len(items); i += batchSz {
		end := i + batchSz
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}

	totalBatches := len(batches)
	skipped := len(relsDone)
	log.Printf("Relationship upload: %d total batches, %d already done, %d remaining", totalBatches, skipped, totalBatches-skipped)

	type workItem struct {
		idx   int
		batch []graph.CreateRelationshipRequest
	}

	work := make(chan workItem, nWorkers*2)
	var wg sync.WaitGroup
	var succeeded, failed, skippedCount atomic.Int64

	for i := 0; i < nWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wi := range work {
				if ctx.Err() != nil {
					return
				}
				res, err := client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				if err != nil {
					log.Printf("  [rels] batch %d error: %v — retrying once", wi.idx, err)
					time.Sleep(500 * time.Millisecond)
					res, err = client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				}
				if err != nil || res == nil {
					log.Printf("  [rels] batch %d failed permanently — saving to rels_failed.jsonl", wi.idx)
					appendRelFailed(stateDir, wi.batch)
					failed.Add(int64(len(wi.batch)))
					continue
				}
				batchFailed := false
				for _, r := range res.Results {
					if !r.Success && r.Error != nil {
						log.Printf("  [rels] batch %d item error: %s", wi.idx, *r.Error)
						batchFailed = true
					}
				}
				if batchFailed {
					appendRelFailed(stateDir, wi.batch)
					failed.Add(int64(res.Failed))
				} else {
					appendRelDone(stateDir, wi.idx)
					succeeded.Add(int64(res.Success))
				}
				n := succeeded.Load() + failed.Load() + skippedCount.Load()
				if n%50000 == 0 && n > 0 {
					log.Printf("  ...%d relationships processed (ok=%d fail=%d skip=%d)",
						n*int64(batchSz), succeeded.Load()*int64(batchSz), failed.Load()*int64(batchSz), skippedCount.Load()*int64(batchSz))
				}
			}
		}()
	}

	go func() {
		for idx, batch := range batches {
			if ctx.Err() != nil {
				break
			}
			if relsDone[idx] {
				skippedCount.Add(1)
				continue
			}
			work <- workItem{idx, batch}
		}
		close(work)
	}()

	wg.Wait()
	log.Printf("  Relationships complete: %d succeeded, %d failed, %d skipped (of %d total batches)",
		succeeded.Load(), failed.Load(), skippedCount.Load(), totalBatches)
	return succeeded.Load() * int64(batchSz), failed.Load() * int64(batchSz)
}

// ─── Seed export (server-free JSONL) ──────────────────────────────────────────

type seedObjectLine struct {
	Type       string         `json:"type"`
	Key        string         `json:"key"`
	Properties map[string]any `json:"properties"`
}

type seedRelationshipLine struct {
	Type       string         `json:"type"`
	SrcKey     string         `json:"srcKey"`
	DstKey     string         `json:"dstKey"`
	Properties map[string]any `json:"properties,omitempty"`
}

// dumpSplitSize is the per-file byte threshold for seed JSONL splitting. It
// matches the Memory CLI blueprint dumper's 50 MB split so files stay under
// GitHub's 100 MB hard limit while the blueprint loader (which reads all
// *.jsonl files, including *.001.jsonl, *.002.jsonl, …) reassembles them.
var dumpSplitSize int64 = 50 * 1024 * 1024

// splitJSONLWriter writes JSONL lines to a sequence of files, splitting at
// dumpSplitSize bytes. Files are named <typeName>.jsonl for the first file, then
// <typeName>.001.jsonl, <typeName>.002.jsonl, … once splitting occurs — the same
// convention as the Memory CLI blueprint dumper.
type splitJSONLWriter struct {
	dir      string
	typeName string
	file     *os.File
	buf      *bufio.Writer
	written  int64
	part     int // 0 = unsplit; 1+ = split sequence
}

func (w *splitJSONLWriter) fileName() string {
	if w.part == 0 {
		return filepath.Join(w.dir, w.typeName+".jsonl")
	}
	return filepath.Join(w.dir, fmt.Sprintf("%s.%03d.jsonl", w.typeName, w.part))
}

func (w *splitJSONLWriter) openNext() error {
	if w.file != nil {
		if err := w.buf.Flush(); err != nil {
			return err
		}
		if err := w.file.Close(); err != nil {
			return err
		}
	}
	if w.part == 0 && w.file == nil {
		// First file: keep the plain name.
	} else if w.part == 0 {
		// First split: rename the plain file to .001.jsonl, then continue at .002.
		oldName := filepath.Join(w.dir, w.typeName+".jsonl")
		w.part = 1
		if err := os.Rename(oldName, w.fileName()); err != nil {
			return fmt.Errorf("rename split file: %w", err)
		}
		w.part++
	} else {
		w.part++
	}
	w.written = 0

	f, err := os.Create(w.fileName())
	if err != nil {
		return fmt.Errorf("create file %s: %w", w.fileName(), err)
	}
	w.file = f
	w.buf = bufio.NewWriter(f)
	return nil
}

func (w *splitJSONLWriter) writeLine(line []byte) error {
	needed := int64(len(line)) + 1 // +1 for newline
	if w.written > 0 && w.written+needed > dumpSplitSize {
		if err := w.openNext(); err != nil {
			return err
		}
	}
	if _, err := w.buf.Write(line); err != nil {
		return err
	}
	if err := w.buf.WriteByte('\n'); err != nil {
		return err
	}
	w.written += needed
	return nil
}

func (w *splitJSONLWriter) close() error {
	if w.file == nil {
		return nil
	}
	if err := w.buf.Flush(); err != nil {
		return err
	}
	return w.file.Close()
}

func writeObjectJSONL(dir, typ string, recs []seedObjectRecord) error {
	w := &splitJSONLWriter{dir: dir, typeName: typ}
	if err := w.openNext(); err != nil {
		return err
	}
	for _, r := range recs {
		data, err := json.Marshal(seedObjectLine{Type: r.Type, Key: r.Key, Properties: r.Properties})
		if err != nil {
			w.close() //nolint:errcheck
			return err
		}
		if err := w.writeLine(data); err != nil {
			return err
		}
	}
	return w.close()
}

func writeRelationshipJSONL(dir, typ string, recs []seedRelationshipRecord) error {
	w := &splitJSONLWriter{dir: dir, typeName: typ}
	if err := w.openNext(); err != nil {
		return err
	}
	for _, r := range recs {
		data, err := json.Marshal(seedRelationshipLine{Type: r.Type, SrcKey: r.SrcKey, DstKey: r.DstKey, Properties: r.Properties})
		if err != nil {
			w.close() //nolint:errcheck
			return err
		}
		if err := w.writeLine(data); err != nil {
			return err
		}
	}
	return w.close()
}

// removeJSONLFiles deletes every non-directory *.jsonl entry in dir. Used to
// clear stale seed files before a fresh export.
func removeJSONLFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// dumpSeed writes a portable blueprint seed (JSONL) to <dir>/seed/objects/<Type>.jsonl
// and <dir>/seed/relationships/<Type>.jsonl. It builds object records first so the
// relationship pass can enforce the "both endpoints exist" (known-refs) rule and
// the SrcKey != DstKey self-loop rule without any server round-trip.
func dumpSeed(dir string, docs []LovDoc, directives []*EUDirective, concepts []*EuroVocConcept, archives []sourceArchive, dataset string) error {
	// Deterministic emission: sort the EU inputs so the seed bytes are stable
	// across regenerations regardless of fetch/cache ordering (the cached
	// directives bypass fetchAllEUData's sort, so sort again at the single
	// choke point for seed export).
	sortDirectives(directives)
	sortConcepts(concepts)

	objRecords := buildSeedObjectRecords(docs, directives, concepts)
	objectKeys := make(map[string]bool, len(objRecords))
	for _, r := range objRecords {
		objectKeys[r.Key] = true
	}
	relRecords := buildSeedRelationshipRecords(docs, directives, objectKeys)

	objByType := make(map[string][]seedObjectRecord)
	for _, r := range objRecords {
		objByType[r.Type] = append(objByType[r.Type], r)
	}
	relByType := make(map[string][]seedRelationshipRecord)
	for _, r := range relRecords {
		if !objectKeys[r.SrcKey] || !objectKeys[r.DstKey] {
			continue
		}
		if r.SrcKey == r.DstKey {
			continue
		}
		relByType[r.Type] = append(relByType[r.Type], r)
	}

	objDir := filepath.Join(dir, "seed", "objects")
	relDir := filepath.Join(dir, "seed", "relationships")
	if err := os.MkdirAll(objDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(relDir, 0755); err != nil {
		return err
	}

	// Remove any stale JSONL files from previous exports. A later run may emit
	// fewer records or split differently, so leftover files would otherwise be
	// read by the blueprint loader as duplicate/outdated records.
	if err := removeJSONLFiles(objDir); err != nil {
		return err
	}
	if err := removeJSONLFiles(relDir); err != nil {
		return err
	}

	objectCount := 0
	objByTypeCounts := make(map[string]int, len(objByType))
	for _, typ := range sortedMapKeys(objByType) {
		recs := objByType[typ]
		if err := writeObjectJSONL(objDir, typ, recs); err != nil {
			return err
		}
		objectCount += len(recs)
		objByTypeCounts[typ] = len(recs)
	}

	relCount := 0
	for _, typ := range sortedMapKeys(relByType) {
		recs := relByType[typ]
		if err := writeRelationshipJSONL(relDir, typ, recs); err != nil {
			return err
		}
		relCount += len(recs)
	}

	log.Printf("Seed export complete: %d objects, %d relationships -> %s", objectCount, relCount, filepath.Join(dir, "seed"))

	// Build the change-detection manifest and diff against any prior one.
	mf := buildManifest(docs, archives, dataset, objectCount, relCount, objByTypeCounts)
	manifestPath := filepath.Join(dir, "seed", "manifest.json")
	if old, ok, err := loadManifest(manifestPath); err != nil {
		log.Printf("  [manifest] cannot read existing manifest: %v", err)
	} else if ok {
		ch := diffManifests(old, mf)
		log.Printf("  [manifest] change detection: added=%d changed=%d removed=%d unchanged=%d", ch.Added, ch.Changed, ch.Removed, ch.Unchanged)
		const maxChanged = 10
		for i, ref := range ch.ChangedRefs {
			if i >= maxChanged {
				log.Printf("  [manifest]   ... and %d more changed", len(ch.ChangedRefs)-maxChanged)
				break
			}
			log.Printf("  [manifest]   changed: %s", ref)
		}
		if ch.Added == 0 && ch.Changed == 0 && ch.Removed == 0 {
			log.Printf("  [manifest] no source changes detected")
		}
	} else {
		log.Printf("  [manifest] no existing manifest; writing initial manifest")
	}
	if err := writeManifest(manifestPath, mf); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// ─── Project helpers ──────────────────────────────────────────────────────────

// ensureProject finds a project by name within the organisation (or the user's
// accessible projects when orgID is empty) and returns its ID; if none exists it
// creates one. Mirrors the CLI's `lovdataEnsureProject`.
func ensureProject(ctx context.Context, client *sdk.Client, orgID, name string) (string, error) {
	list, err := client.Projects.List(ctx, &projects.ListOptions{OrgID: orgID})
	if err != nil {
		return "", fmt.Errorf("list projects: %w", err)
	}
	for _, p := range list {
		if p.Name == name {
			log.Printf("  Found existing project %q (ID: %s)", name, p.ID)
			return p.ID, nil
		}
	}
	proj, err := client.Projects.Create(ctx, &projects.CreateProjectRequest{
		Name:  name,
		OrgID: orgID,
	})
	if err != nil {
		return "", fmt.Errorf("create project %q: %w", name, err)
	}
	log.Printf("  Created project %q (ID: %s)", proj.Name, proj.ID)
	return proj.ID, nil
}

// deleteProject deletes a project by ID (used by --cleanup).
func deleteProject(ctx context.Context, client *sdk.Client, projectID string) error {
	return client.Projects.Delete(ctx, projectID)
}

// ─── main ─────────────────────────────────────────────────────────────────────

func main() {
	cfg := parseConfig()

	dumpMode := cfg.dumpSeedDir != ""

	if !dumpMode {
		if err := cfg.validate(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
			flag.Usage()
			os.Exit(1)
		}
	}

	os.MkdirAll(cfg.cacheDir, 0755) //nolint:errcheck

	var client *sdk.Client
	if !dumpMode {
		os.MkdirAll(cfg.stateDir, 0755) //nolint:errcheck

		var err error
		client, err = sdk.New(sdk.Config{
			ServerURL:  cfg.serverURL,
			ProjectID:  cfg.projectID,
			OrgID:      cfg.orgID,
			HTTPClient: &http.Client{Timeout: 5 * time.Minute},
			Auth:       sdk.AuthConfig{Mode: "apikey", APIKey: cfg.token},
		})
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("State directory: %s", cfg.stateDir)
		log.Printf("Cache directory: %s", cfg.cacheDir)
		if cfg.limit > 0 {
			log.Printf("Document limit: %d per dataset", cfg.limit)
		}
	} else {
		log.Printf("Norwegian Law Seeder → server-free seed export")
		log.Printf("Output directory: %s", cfg.dumpSeedDir)
		log.Printf("Cache directory: %s", cfg.cacheDir)
		if cfg.limit > 0 {
			log.Printf("Document limit: %d per dataset", cfg.limit)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %s — shutting down gracefully (state preserved)", sig)
		cancel()
	}()

	// Resolve the target project: --project overrides; otherwise find-or-create
	// by name within the org. --download-only skips this entirely: it only
	// downloads and caches, so no project exists (or is needed) for ingestion.
	if !dumpMode && !cfg.downloadOnly {
		if cfg.projectID == "" {
			resolved, err := ensureProject(ctx, client, cfg.orgID, cfg.projectName)
			if err != nil {
				log.Fatalf("resolve project: %v", err)
			}
			cfg.projectID = resolved
		}
		client.SetContext(cfg.orgID, cfg.projectID)
		log.Printf("Norwegian Law Seeder → %s (project: %s)", cfg.serverURL, cfg.projectID)
	}

	var allDocs []LovDoc
	var directives []*EUDirective
	var concepts []*EuroVocConcept
	var sourceArchives []sourceArchive

	cachePathDocs := filepath.Join(cfg.cacheDir, "parsed_docs.json")
	cachePathDirectives := filepath.Join(cfg.cacheDir, "parsed_directives.json")
	cachePathConcepts := filepath.Join(cfg.cacheDir, "parsed_concepts.json")

	if cfg.ingestOnly {
		log.Printf("--ingest-only: loading from cache in %s ...", cfg.cacheDir)

		bDocs, err := os.ReadFile(cachePathDocs)
		if err != nil {
			log.Fatalf("read cached docs: %v", err)
		}
		json.Unmarshal(bDocs, &allDocs) //nolint:errcheck

		if bDir, err := os.ReadFile(cachePathDirectives); err == nil {
			json.Unmarshal(bDir, &directives) //nolint:errcheck
		}
		if bConc, err := os.ReadFile(cachePathConcepts); err == nil {
			json.Unmarshal(bConc, &concepts) //nolint:errcheck
		}

		log.Printf("  Loaded: docs=%d directives=%d concepts=%d", len(allDocs), len(directives), len(concepts))
	} else {
		log.Printf("Phase 1: Loading Lovdata documents (limit=%d, dataset=%s) ...", cfg.limit, cfg.dataset)

		if cfg.dataset == "laws" || cfg.dataset == "both" {
			log.Println("  Downloading laws ...")
			docs, arch, err := loadDataset(lawsURL, "Law", cfg.limit, cfg.cacheDir)
			if err != nil {
				log.Fatalf("load laws: %v", err)
			}
			log.Printf("  Parsed %d laws", len(docs))
			allDocs = append(allDocs, docs...)
			sourceArchives = append(sourceArchives, arch)
		}
		if cfg.dataset == "regulations" || cfg.dataset == "both" {
			log.Println("  Downloading regulations ...")
			docs, arch, err := loadDataset(regsURL, "Regulation", cfg.limit, cfg.cacheDir)
			if err != nil {
				log.Fatalf("load regulations: %v", err)
			}
			log.Printf("  Parsed %d regulations", len(docs))
			allDocs = append(allDocs, docs...)
			sourceArchives = append(sourceArchives, arch)
		}
		log.Printf("  Total docs: %d", len(allDocs))

		if !cfg.skipEU {
			// Use cached directives if available
			if bDir, err := os.ReadFile(cachePathDirectives); err == nil && len(bDir) > 10 {
				var cached []*EUDirective
				if jsonErr := json.Unmarshal(bDir, &cached); jsonErr == nil && len(cached) > 0 {
					directives = cached
					log.Printf("Phase 2: Using cached EU directives (count=%d, skipping re-fetch)", len(directives))
					if bConc, err2 := os.ReadFile(cachePathConcepts); err2 == nil && len(bConc) > 10 {
						json.Unmarshal(bConc, &concepts) //nolint:errcheck
					}
				}
			}
			if len(directives) == 0 {
				log.Println("Phase 2: Fetching EU directive metadata from EUR-Lex ...")
				directives, concepts = fetchAllEUData(ctx, allDocs, cfg.euLimit)
			}
		} else {
			log.Println("Phase 2: EU enrichment skipped (--skip-eu)")
		}

		// Save to cache
		log.Println("Saving parsed data to cache ...")
		if bDocs, err := json.MarshalIndent(allDocs, "", "  "); err == nil {
			os.WriteFile(cachePathDocs, bDocs, 0644) //nolint:errcheck
		}
		if bDir, err := json.MarshalIndent(directives, "", "  "); err == nil {
			os.WriteFile(cachePathDirectives, bDir, 0644) //nolint:errcheck
		}
		if bConc, err := json.MarshalIndent(concepts, "", "  "); err == nil {
			os.WriteFile(cachePathConcepts, bConc, 0644) //nolint:errcheck
		}

		if cfg.downloadOnly {
			log.Println("Data downloaded and cached successfully. Exiting (--download-only).")
			return
		}
	}

	if cfg.limit > 0 && len(allDocs) > cfg.limit*2 {
		allDocs = allDocs[:cfg.limit*2]
	}

	if dumpMode {
		if err := dumpSeed(cfg.dumpSeedDir, allDocs, directives, concepts, sourceArchives, cfg.dataset); err != nil {
			log.Fatalf("seed export: %v", err)
		}
		return
	}

	state := loadState(cfg.stateDir)
	log.Printf("Resuming from phase: %s", state.Phase)

	var idMap map[string]string

	if state.Phase == phaseObjectsPending {
		log.Println("Phase 3: Ingesting objects ...")
		idMap = ingestObjects(ctx, client.Graph, allDocs, directives, concepts, cfg.batchSz, cfg.workers)

		if ctx.Err() != nil {
			log.Println("Interrupted during object phase — state NOT advanced (will re-do objects on resume)")
			return
		}

		log.Printf("Saving idmap.json (%d entries) ...", len(idMap))
		saveIDMap(cfg.stateDir, idMap)
		state.Phase = phaseObjectsDone
		saveState(cfg.stateDir, state)
		log.Printf("Object phase complete — idMap has %d entries", len(idMap))

		state.Phase = phaseRelsPending
		saveState(cfg.stateDir, state)
		log.Println("Phase 4: Ingesting relationships ...")
		relSucceeded, relFailed := ingestRelationships(ctx, client.Graph, allDocs, directives, idMap, cfg.batchSz, cfg.workers, cfg.stateDir)
		log.Printf("Relationship phase complete — succeeded=%d failed=%d", relSucceeded, relFailed)

	} else if state.Phase == phaseObjectsDone || state.Phase == phaseRelsPending {
		log.Println("Object phase already complete — loading idmap.json from disk")
		var err error
		idMap, err = loadIDMap(cfg.stateDir)
		if err != nil {
			log.Fatalf("Cannot load idmap.json: %v", err)
		}
		log.Printf("  idMap loaded: %d entries", len(idMap))

		state.Phase = phaseRelsPending
		saveState(cfg.stateDir, state)
		log.Println("Phase 4: Ingesting relationships ...")
		relSucceeded, relFailed := ingestRelationships(ctx, client.Graph, allDocs, directives, idMap, cfg.batchSz, cfg.workers, cfg.stateDir)
		log.Printf("Relationship phase complete — succeeded=%d failed=%d", relSucceeded, relFailed)

	} else {
		log.Println("All phases already complete. Run with a fresh state directory to re-seed.")
		return
	}

	state.Phase = phaseDone
	saveState(cfg.stateDir, state)
	log.Println("Done.")

	if cfg.cleanup {
		log.Printf("Cleaning up project %s ...", cfg.projectID)
		if err := deleteProject(ctx, client, cfg.projectID); err != nil {
			log.Printf("WARN: cleanup failed: %v", err)
		}
	}
}
