package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// ─── Stortinget forarbeid full-text (phase B) ─────────────────────────────────
//
// The Stortinget data API (NLOD 2.0) serves an index of publications
// (https://data.stortinget.no/eksport/publikasjoner) and per-publication XML
// (https://data.stortinget.no/eksport/publikasjon?publikasjonid=<id>). This file
// builds a cached index, matches our anchor-derived slugs to Stortinget ids by
// index lookup (NOT string rewriting — number padding, the inno prefix, and the
// s/l suffixes are load-bearing), fetches + caches the raw XML, and extracts
// Markdown text.

const stortingetIndexBase = "https://data.stortinget.no/eksport/publikasjoner"
const stortingetPubBase = "https://data.stortinget.no/eksport/publikasjon"

// stortingetIndexEntry is one element of the publikasjoner_liste.
type stortingetIndexEntry struct {
	ID                     string   `json:"id"`
	Tittel                 string   `json:"tittel"`
	Type                   int      `json:"type"`
	PublikasjonformatListe []string `json:"publikasjonformat_liste"`
	PublikasjonsPdfer      []string `json:"publikasjonsPdfer"`
	Dato                   string   `json:"dato"`
	TilgjengeligDato       string   `json:"tilgjengelig_dato"`
}

// stortingetPub is a flattened, index-keyed publication record.
type stortingetPub struct {
	ID     string
	Tittel string
	PDFs   []string
}

// stortingetIndex maps publication id -> pub.
type stortingetIndex map[string]stortingetPub

// stortingetSessions returns the parliamentary sessions to index (1999-2000 …
// 2026-2027). pre-1999 forarbeid are out of scope (Stortinget's JSON archive
// starts ~1998 and the older DTD shapes are less reliable).
func stortingetSessions() []string {
	var out []string
	for y := 1999; y <= 2026; y++ {
		out = append(out, fmt.Sprintf("%d-%d", y, y+1))
	}
	return out
}

// stortingetTypes returns the publication types we index. proposisjoner/NOU are
// served by regjeringen.no (no API), so only innstilling + lovvedtak are fetched.
func stortingetTypes() []string {
	return []string{"innstilling", "lovvedtak"}
}

// indexDir returns the cache directory for the raw index JSON.
func stortingetIndexDir(cacheDir string) string {
	return filepath.Join(cacheDir, "stortinget-index")
}

// parseStortingetIndexJSON decodes one index response body. publikasjoner_liste
// is a LIST; a single-entry object form is tolerated defensively.
func parseStortingetIndexJSON(data []byte) ([]stortingetIndexEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	listRaw, ok := raw["publikasjoner_liste"]
	if !ok {
		return nil, fmt.Errorf("no publikasjoner_liste")
	}
	// Try list first.
	var list []stortingetIndexEntry
	if err := json.Unmarshal(listRaw, &list); err == nil {
		return list, nil
	}
	// Single-object fallback.
	var single stortingetIndexEntry
	if err := json.Unmarshal(listRaw, &single); err == nil {
		return []stortingetIndexEntry{single}, nil
	}
	return nil, fmt.Errorf("publikasjoner_liste is neither list nor object")
}

// loadStortingetIndex builds (or loads from cache) the id→pub index for the
// configured sessions and types. Cached JSON under <cache>/stortinget-index/ is
// preferred so a re-dump needs no network.
func loadStortingetIndex(ctx context.Context, cacheDir string) (stortingetIndex, error) {
	idx := stortingetIndex{}
	dir := stortingetIndexDir(cacheDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	for _, typ := range stortingetTypes() {
		for _, sess := range stortingetSessions() {
			cacheFile := filepath.Join(dir, fmt.Sprintf("%s-%s.json", typ, sess))
			var data []byte
			if b, rerr := os.ReadFile(cacheFile); rerr == nil {
				data = b
			} else {
				var err error
				data, err = fetchStortingetIndex(ctx, typ, sess)
				if err != nil {
					// Tolerate per-session failures: skip and continue (log only).
					log.Printf("  [stortinget] index %s %s: %v (skipping)", typ, sess, err)
					continue
				}
				if werr := os.WriteFile(cacheFile, data, 0644); werr != nil {
					log.Printf("  [stortinget] cache write %s: %v", cacheFile, werr)
				}
			}
			entries, err := parseStortingetIndexJSON(data)
			if err != nil {
				log.Printf("  [stortinget] index parse %s %s: %v (skipping)", typ, sess, err)
				continue
			}
			for _, e := range entries {
				// Normalize the id to lowercase: a few sessions carry a stray
				// capitalised "Inns-" prefix ("Inns-201516-002") that is otherwise
				// the same publication. Lowercasing makes index lookup and cache
				// filenames deterministic.
				id := strings.ToLower(e.ID)
				idx[id] = stortingetPub{ID: id, Tittel: e.Tittel, PDFs: e.PublikasjonsPdfer}
			}
			// Pacing is enforced centrally by stortingetRL inside stortingetGet.
		}
	}
	log.Printf("  [stortinget] index: %d publications", len(idx))
	return idx, nil
}

// stortingetRateLimiter enforces a minimum interval between Stortinget API
// calls. The endpoint rate-limits at ~100 calls/min and intermittently returns
// HTTP 429; ~1000ms keeps us comfortably under the limit while still fast enough
// to fetch a few hundred publications. Fetches are also resumable via the cache,
// so a re-dump never re-fetches a publication that already succeeded.
type stortingetRateLimiter struct {
	mu   sync.Mutex
	last time.Time
	min  time.Duration
}

func (r *stortingetRateLimiter) wait() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := time.Since(r.last); d < r.min {
		time.Sleep(r.min - d)
	}
	r.last = time.Now()
}

var stortingetRL = &stortingetRateLimiter{min: 1000 * time.Millisecond}

// nextBackoff doubles a retry backoff, capped at 30s.
func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// stortingetGet performs a rate-limited GET with up to `attempts` attempts,
// retrying transient failures (network errors, HTTP 429, HTTP 5xx) with
// exponential backoff. On HTTP 429/503 the Retry-After header (seconds or
// HTTP-date) is honoured when present, still capped at 30s; otherwise the
// backoff doubles (1s → 30s). Hard statuses (404, etc.) abort immediately. The
// returned error carries the HTTP status so fetch failures can be reported
// precisely. Each retry is logged so a long run never appears hung.
func stortingetGet(ctx context.Context, rawURL, accept string, attempts int) ([]byte, error) {
	var lastErr error
	backoff := 1 * time.Second
	for i := 1; i <= attempts; i++ {
		stortingetRL.wait()
		req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", "NorwegianLawSeeder/1.0")
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			if i == attempts {
				continue
			}
			log.Printf("  [stortinget] GET %s attempt %d/%d: %v (retrying in %s)", rawURL, i, attempts, err, backoff)
			if !sleepCtx(ctx, backoff) {
				return nil, ctx.Err()
			}
			backoff = nextBackoff(backoff)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if i == attempts {
				continue
			}
			log.Printf("  [stortinget] GET %s attempt %d/%d: read %v (retrying in %s)", rawURL, i, attempts, readErr, backoff)
			if !sleepCtx(ctx, backoff) {
				return nil, ctx.Err()
			}
			backoff = nextBackoff(backoff)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			d := backoff
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, perr := strconv.Atoi(strings.TrimSpace(ra)); perr == nil {
					d = time.Duration(secs) * time.Second
				} else if t, perr := http.ParseTime(ra); perr == nil {
					d = time.Until(t)
				}
			}
			if d < time.Second {
				d = time.Second
			}
			if d > 30*time.Second {
				d = 30 * time.Second
			}
			if i == attempts {
				continue
			}
			log.Printf("  [stortinget] GET %s attempt %d/%d: HTTP %d (retrying in %s)", rawURL, i, attempts, resp.StatusCode, d)
			if !sleepCtx(ctx, d) {
				return nil, ctx.Err()
			}
			backoff = nextBackoff(backoff)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return body, nil
	}
	return nil, lastErr
}

// sleepCtx sleeps for d, returning false if the context is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// fetchStortingetIndex GETs one index page (rate-limited, retried).
func fetchStortingetIndex(ctx context.Context, typ, sess string) ([]byte, error) {
	q := url.Values{
		"publikasjontype": {typ},
		"sesjonid":        {sess},
		"format":          {"json"},
	}
	return stortingetGet(ctx, stortingetIndexBase+"?"+q.Encode(), "application/json", 4)
}

// sessionCodeToCompact converts "2004-2005" -> "200405" (first year + the
// 2-digit next-year suffix), matching the Stortinget id's compact session code.
func sessionCodeToCompact(session string) string {
	parts := strings.Split(session, "-")
	if len(parts) == 2 && len(parts[0]) == 4 && len(parts[1]) == 4 {
		return parts[0] + parts[1][2:]
	}
	return ""
}

// parseInnstillingSlug extracts (variant, number, sessionCode) from an
// innstilling slug. Both orderings exist in the corpus:
//
//	inns-o-80-200405   -> variant "o", number "80",  session "200405"
//	inns-100-l-201213  -> number "100", variant "l", session "201213"
func parseInnstillingSlug(parts []string) (variant, num, sessionCode string) {
	if len(parts) < 4 {
		return "", "", ""
	}
	sessionCode = parts[len(parts)-1]
	middle := parts[1 : len(parts)-1]
	var nums []string
	for _, t := range middle {
		if isDigits(t) {
			nums = append(nums, t)
		} else {
			variant = t
		}
	}
	if len(nums) == 1 {
		num = nums[0]
	}
	return variant, num, sessionCode
}

// innstillingCandidates derives the Stortinget publication ids to try for a
// Lovdata innstilling slug. The variant letter is load-bearing and maps to a
// *specific* id shape, NOT a simple prefix swap:
//
//	"o"  -> "inno-<sess>-<NNN>"          (Innst. O. nr. — Odelstinget, pre-2009)
//	"l"  -> "inns-<sess>-<NNN>l"         (Innst. NNN L — 2016-2017 onward)
//	"s"  -> "inns-<sess>-<NNN>s"         (Innst. NNN S — 2016-2017 onward)
//	""   -> "inns-<sess>-<NNN>"          (plain innstilling)
//
// The suffix forms only exist from the 2016-2017 session on; earlier post-2009
// sessions kept the L/S classification out of the id, so for "l"/"s" we also try
// the plain form and let the title cross-check settle it (there is no ambiguity
// because the index never holds both "<NNN>l" and "<NNN>" for one number).
func innstillingCandidates(variant, sess, num string) []string {
	padded := zeroPad3(num)
	switch strings.ToLower(variant) {
	case "o":
		return []string{"inno-" + sess + "-" + padded}
	case "l":
		return []string{"inns-" + sess + "-" + padded + "l", "inns-" + sess + "-" + padded}
	case "s":
		return []string{"inns-" + sess + "-" + padded + "s", "inns-" + sess + "-" + padded}
	default:
		return []string{"inns-" + sess + "-" + padded}
	}
}

// zeroPad3 zero-pads a number to at least 3 digits ("80" -> "080").
func zeroPad3(num string) string {
	if n, err := strconv.Atoi(num); err == nil {
		return fmt.Sprintf("%03d", n)
	}
	return num
}

// matchStortingetPub matches a prepWork to a Stortinget publication by index
// lookup, cross-checking the title against our anchor name. Returns the pub and
// ok; on failure, a short reason.
func matchStortingetPub(pw prepWork, idx stortingetIndex) (stortingetPub, bool, string) {
	if pw.Year != "" && pw.Year < "1999" {
		return stortingetPub{}, false, "pre_1999"
	}
	sess := sessionCodeToCompact(pw.Session)
	if sess == "" {
		return stortingetPub{}, false, "no_session"
	}
	parts := strings.Split(pw.Slug, "-")
	var candidates []string
	switch pw.PrepType {
	case "innstilling":
		variant, num, _ := parseInnstillingSlug(parts)
		if num == "" {
			return stortingetPub{}, false, "no_number"
		}
		candidates = innstillingCandidates(variant, sess, num)
	case "lovvedtak":
		if pw.DocNumber == "" {
			return stortingetPub{}, false, "no_number"
		}
		candidates = append(candidates, "vedtak-"+sess+"-"+zeroPad3(pw.DocNumber))
	default:
		return stortingetPub{}, false, "not_stortinget_type"
	}
	for _, cid := range candidates {
		if pub, ok := idx[cid]; ok {
			if titleMatches(pub.Tittel, pw.Name, pw.DocNumber) {
				return pub, true, ""
			}
			return stortingetPub{}, false, "title_mismatch:" + cid
		}
	}
	return stortingetPub{}, false, "no_index_entry:" + strings.Join(candidates, "|")
}

// titleMatches loosely cross-checks the Stortinget title against our anchor name:
// both must carry the same document number token, and the title must be non-empty.
func titleMatches(stortingetTitle, ourName, docNumber string) bool {
	st := strings.ToLower(stortingetTitle)
	if strings.TrimSpace(st) == "" {
		return false
	}
	if docNumber != "" {
		// The number must appear as a standalone token (word-boundary-ish) in the
		// Stortinget title, so "Innst. 80" does not match "Innst. 180".
		numRe := regexp.MustCompile(`(^|[^0-9])` + regexp.QuoteMeta(docNumber) + `([^0-9]|$)`)
		if !numRe.MatchString(st) {
			return false
		}
	}
	return true
}

// fetchStortingetXML fetches (or loads from cache) the raw publication XML.
// Fetches are rate-limited and retried; hard statuses (404) abort and are
// reported rather than silently tolerated.
func fetchStortingetXML(ctx context.Context, cacheDir, id string) ([]byte, error) {
	dir := stortingetIndexDir(cacheDir)
	cacheFile := filepath.Join(dir, id+".xml")
	if b, err := os.ReadFile(cacheFile); err == nil {
		return b, nil
	}
	log.Printf("  [stortinget] fetching %s", id)
	body, err := stortingetGet(ctx, stortingetPubBase+"?publikasjonid="+url.QueryEscape(id), "application/xml, text/xml, */*", 5)
	if err != nil {
		return nil, fmt.Errorf("%v fetching %s", err, id)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty body for %s", id)
	}
	if werr := os.WriteFile(cacheFile, body, 0644); werr != nil {
		log.Printf("  [stortinget] cache write %s: %v", cacheFile, werr)
	}
	return body, nil
}

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ─── XML → Markdown ───────────────────────────────────────────────────────────

// headingTags are XML tags rendered as markdown headings.
var stortingetHeadingTags = map[string]bool{
	"tit": true, "tittel": true, "stikktittel": true, "navn": true,
	"doktit": true,
}

// blockTags are leaf text elements rendered as paragraph blocks.
var stortingetBlockTags = map[string]bool{
	"a": true, "uttal": true, "endring": true, "forsl": true,
	"tilstortinget": true, "innst": true, "aar": true,
}

// quoteTags are rendered as blockquotes.
var stortingetQuoteTags = map[string]bool{
	"sitat": true, "blksit": true, "komtilr": true,
}

// extractStortingetMarkdown converts a Stortinget publication XML body into
// faithful Markdown, tolerating the two DTD generations (lowercase innstilling
// tags vs Capitalized Innstilling/VedtakTilLov tags) and their structural
// elements (paragraf, ledd, uttal, kapittel, table, sitat, …). Structural
// wrappers (paragraf, kapittel, vedtaktillov, …) are recursed into so nested
// headings are preserved.
func extractStortingetMarkdown(xmlData []byte) (string, error) {
	root, err := html.Parse(strings.NewReader(string(xmlData)))
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
			return
		}
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}
		tag := strings.ToLower(n.Data)
		switch {
		case stortingetHeadingTags[tag]:
			if text := strings.TrimSpace(nodeText(n)); text != "" {
				sb.WriteString("### " + text + "\n\n")
			}
			return
		case stortingetQuoteTags[tag]:
			if text := strings.TrimSpace(nodeText(n)); text != "" {
				sb.WriteString("> " + strings.ReplaceAll(text, "\n", "\n> ") + "\n\n")
			}
			return
		case stortingetBlockTags[tag]:
			if text := strings.TrimSpace(nodeText(n)); text != "" {
				sb.WriteString(text + "\n\n")
			}
			return
		case tag == "table":
			sb.WriteString(renderStortingetTable(n))
			return
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}
	}
	walk(root)
	return strings.TrimSpace(sb.String()), nil
}

// renderStortingetTable renders a <table><row><entry>… structure as a
// pipe-separated markdown table (one line per row).
func renderStortingetTable(table *html.Node) string {
	var rows []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "row") {
			var cells []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && strings.EqualFold(c.Data, "entry") {
					cells = append(cells, strings.TrimSpace(nodeText(c)))
				}
			}
			if len(cells) > 0 {
				rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n") + "\n\n"
}

// ─── <kildedok> → DERIVES_FROM ────────────────────────────────────────────────

// kildedokRefPattern matches a forarbeid reference inside a <kildedok> element:
// "Prop. 158 L (2024–2025)", "Ot.prp. nr. 76 (2008-2009)", "St.meld. nr. 1
// (2004-2005)", "Innst. 33 L (2025–2026)".
var kildedokRefPattern = regexp.MustCompile(`(?i)(ot\.prp\.|prop\.|st\.meld\.|st\.prp\.|innst\.)\s*(?:nr\.?\s*)?(\d{1,4})\s*(?:([A-ZÆØÅ]{1,3})\b)?\s*(?:\(|–|\s)\s*(\d{4})`)

// kildedokNOUPattern matches "NOU 2017:15" (year before report number).
var kildedokNOUPattern = regexp.MustCompile(`(?i)\bnou\s*(\d{4})\s*[:/\s]\s*(\d{1,3})`)

// kildedokToSlugs maps a <kildedok> text blob to canonical forarbeid slugs. It is
// conservative: only the known prefix forms are emitted.
func kildedokToSlugs(text string) []string {
	set := make(map[string]bool)
	// NOU (year-first form).
	for _, m := range kildedokNOUPattern.FindAllStringSubmatch(text, -1) {
		set["nou-"+m[1]+"-"+m[2]] = true
	}
	for _, m := range kildedokRefPattern.FindAllStringSubmatch(text, -1) {
		prefix := strings.ToLower(strings.TrimRight(m[1], "."))
		num := m[2]
		variant := strings.ToLower(m[3])
		year := m[4]
		var slug string
		switch prefix {
		case "ot.prp":
			slug = "otprp-" + num + "-" + year + sessionTail(year)
		case "prop":
			if variant != "" {
				slug = "prop-" + num + "-" + variant + "-" + year + sessionTail(year)
			} else {
				slug = "prop-" + num + "-" + year + sessionTail(year)
			}
		case "st.meld":
			slug = "meld-st-" + num + "-" + year + sessionTail(year)
		case "st.prp":
			slug = "stprp-" + num + "-" + year + sessionTail(year)
		case "innst":
			if variant != "" {
				slug = "inns-" + num + "-" + variant + "-" + year + sessionTail(year)
			} else {
				slug = "inns-" + num + "-" + year + sessionTail(year)
			}
		default:
			continue
		}
		set[slug] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// sessionTail returns the 2-digit "next year" suffix for a session code starting
// at the given 4-digit year, e.g. "2024" -> "25" (2024-2025).
func sessionTail(year string) string {
	y, err := strconv.Atoi(year)
	if err != nil {
		return ""
	}
	ny := (y + 1) % 100
	if ny < 10 {
		return "0" + strconv.Itoa(ny)
	}
	return strconv.Itoa(ny)
}

// extractKildedokSlugs returns the canonical slugs referenced by a publication's
// <kildedok> elements (case-insensitive tag), deduped and sorted.
func extractKildedokSlugs(xmlData []byte) []string {
	set := make(map[string]bool)
	root, err := html.Parse(strings.NewReader(string(xmlData)))
	if err != nil {
		return nil
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "kildedok") {
			for _, s := range kildedokToSlugs(nodeText(n)) {
				set[s] = true
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// stortingetPubURL returns a human-facing source URL for a publication: the first
// PDF if present, else the data-API publication URL.
func stortingetPubURL(pub stortingetPub) string {
	if len(pub.PDFs) > 0 {
		return pub.PDFs[0]
	}
	return stortingetPubBase + "?publikasjonid=" + url.QueryEscape(pub.ID)
}

// prepFullTextStats aggregates phase-B enrichment for the manifest.
type prepFullTextStats struct {
	WithFullText    int
	MatchedByType   map[string]int
	UnmatchedByType map[string]int
	FetchFailures   []string
}

// enrichPreparatoryWorks matches the Stortinget-served subset of prepWorks to
// publication ids, fetches + caches full text, and extracts Markdown + kildedok.
// It returns the enriched works (deterministic order) and the summary stats.
// Unmatched works keep content_available:false and source:"lovdata-ref".
func enrichPreparatoryWorks(ctx context.Context, cacheDir string, preps []prepWork) ([]prepWork, prepFullTextStats) {
	stats := prepFullTextStats{
		MatchedByType:   make(map[string]int),
		UnmatchedByType: make(map[string]int),
	}
	idx, err := loadStortingetIndex(ctx, cacheDir)
	if err != nil {
		log.Printf("  [stortinget] index load failed: %v (no full text)", err)
		return preps, stats
	}

	for i := range preps {
		pw := &preps[i]
		pw.Source = "lovdata-ref"
		pw.SourceURL = "https://lovdata.no/forarbeid/" + pw.Slug

		pub, ok, reason := matchStortingetPub(*pw, idx)
		if !ok {
			stats.UnmatchedByType[pw.PrepType]++
			if reason != "" {
				log.Printf("  [stortinget] unmatched %s: %s", pw.Slug, reason)
			}
			continue
		}

		xmlData, err := fetchStortingetXML(ctx, cacheDir, pub.ID)
		if err != nil {
			// A fetch failure is a transport/enrichment problem, NOT a match
			// failure — it is recorded in FetchFailures only, so
			// unmatched_by_type keeps counting only genuine index-match misses.
			stats.FetchFailures = append(stats.FetchFailures, pub.ID+": "+err.Error())
			log.Printf("  [stortinget] fetch %s: %v", pub.ID, err)
			continue
		}
		content, err := extractStortingetMarkdown(xmlData)
		if err != nil || strings.TrimSpace(content) == "" {
			// Same: an extraction failure is not an index-match failure.
			stats.FetchFailures = append(stats.FetchFailures, pub.ID+": extract failed")
			log.Printf("  [stortinget] extract %s: %v", pub.ID, err)
			continue
		}

		pw.StortingetID = pub.ID
		pw.Title = pub.Tittel
		pw.Content = content
		pw.ContentAvailable = true
		pw.Source = "stortinget"
		pw.SourceURL = stortingetPubURL(pub)
		pw.SourceHash = sha256HexBytes(xmlData)
		pw.KildedokSlugs = extractKildedokSlugs(xmlData)
		stats.WithFullText++
		stats.MatchedByType[pw.PrepType]++
	}
	sort.Strings(stats.FetchFailures)
	return preps, stats
}

// buildDerivesFromEdges emits DERIVES_FROM edges (innstilling/lovvedtak ->
// kildedok proposisjon/NOU) for enriched works whose <kildedok> targets are
// themselves present in the corpus. Self-loops and dangling targets are skipped
// so lovcheck V2 stays 0. Output is deterministic: preps arrive slug-sorted and
// KildedokSlugs are sorted, and the result is deduped.
func buildDerivesFromEdges(preps []prepWork) []seedRelationshipRecord {
	slugSet := make(map[string]bool, len(preps))
	for _, pw := range preps {
		slugSet[pw.Slug] = true
	}
	var edges []seedRelationshipRecord
	for _, pw := range preps {
		if !pw.ContentAvailable {
			continue
		}
		// Sort a copy so edge order is deterministic even if the caller's
		// KildedokSlugs arrive unsorted.
		targets := append([]string(nil), pw.KildedokSlugs...)
		sort.Strings(targets)
		for _, kslug := range targets {
			if kslug == pw.Slug || !slugSet[kslug] {
				continue
			}
			edges = append(edges, seedRelationshipRecord{
				Type:       "DERIVES_FROM",
				SrcKey:     "forarbeid/" + pw.Slug,
				DstKey:     "forarbeid/" + kslug,
				Properties: map[string]any{},
			})
		}
	}
	return dedupeSeedRelationships(edges)
}
