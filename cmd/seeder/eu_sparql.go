package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ─── SPARQL access ─────────────────────────────────────────────────────────────
//
// The Publications Office SPARQL endpoint (CELLAR) is the authoritative source
// for EU directive metadata. The legacy EUR-Lex HTML page (parseEURLex) no longer
// carries this markup, so metadata is fetched here and the HTML path is kept
// only as a last-resort fallback.

// sparqlQuery POSTs a query to the CELLAR SPARQL endpoint and returns the result
// bindings as rows of var-name → literal/URI value. Accept is forced to
// application/sparql-results+json (the default output is HTML).
func sparqlQuery(ctx context.Context, query string) ([]map[string]string, error) {
	form := url.Values{"query": {query}}
	req, err := http.NewRequestWithContext(ctx, "POST", cellarSPQL, strings.NewReader(form.Encode()))
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SPARQL HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseSPARQLBindings(body)
}

// parseSPARQLBindings decodes a SPARQL JSON results body into rows of
// var-name → value. Separated out so it can be unit-tested offline.
func parseSPARQLBindings(data []byte) ([]map[string]string, error) {
	var sr struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &sr); err != nil {
		return nil, fmt.Errorf("SPARQL JSON decode: %w", err)
	}
	rows := make([]map[string]string, 0, len(sr.Results.Bindings))
	for _, b := range sr.Results.Bindings {
		row := make(map[string]string, len(b))
		for k, v := range b {
			row[k] = v.Value
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// directiveMetaQuery builds the all-metadata SPARQL query for a CELEX id.
func directiveMetaQuery(celex string) string {
	return fmt.Sprintf(`PREFIX cdm:  <http://publications.europa.eu/ontology/cdm#>
PREFIX skos: <http://www.w3.org/2004/02/skos/core#>
PREFIX xsd:  <http://www.w3.org/2001/XMLSchema#>
PREFIX lang: <http://publications.europa.eu/resource/authority/language/>
SELECT ?title ?dateDocument ?dateEffect ?resourceTypeLabel ?authorLabel ?dgLabel
       ?subjectLabel ?directoryLabel ?legalBasisCelex ?docId ?eurovoc ?procedureRef ?citedCelex ?amenderCelex
WHERE {
  ?work cdm:resource_legal_id_celex %q^^xsd:string .
  OPTIONAL { ?expr cdm:expression_belongs_to_work ?work ; cdm:expression_uses_language lang:ENG ; cdm:expression_title ?title }
  OPTIONAL { ?work cdm:work_date_document ?dateDocument }
  OPTIONAL { ?work cdm:resource_legal_date_entry-into-force ?dateEffect }
  OPTIONAL { ?work cdm:work_has_resource-type ?resourceType . ?resourceType skos:prefLabel ?resourceTypeLabel . FILTER(LANG(?resourceTypeLabel)="en") }
  OPTIONAL { ?work cdm:work_created_by_agent ?author . ?author skos:prefLabel ?authorLabel . FILTER(LANG(?authorLabel)="en") }
  OPTIONAL { ?work cdm:resource_legal_responsibility_of_agent ?dg . ?dg skos:prefLabel ?dgLabel . FILTER(LANG(?dgLabel)="en") }
  OPTIONAL { ?work cdm:resource_legal_is_about_subject-matter ?subject . ?subject skos:prefLabel ?subjectLabel . FILTER(LANG(?subjectLabel)="en") }
  OPTIONAL { ?work cdm:resource_legal_is_about_concept_directory-code ?directory . ?directory skos:prefLabel ?directoryLabel . FILTER(LANG(?directoryLabel)="en") }
  OPTIONAL { ?work cdm:resource_legal_based_on_resource_legal ?legalBasis . ?legalBasis cdm:resource_legal_id_celex ?legalBasisCelex }
  OPTIONAL { ?work cdm:work_id_document ?docId . FILTER(STRSTARTS(STR(?docId),"oj:")) }
  OPTIONAL { ?work cdm:work_is_about_concept_eurovoc ?eurovoc }
  OPTIONAL { ?dossier cdm:dossier_produces_resource_legal ?work ; cdm:procedure_code_interinstitutional_reference_procedure ?procedureRef }
  OPTIONAL { ?work cdm:work_cites_work ?cited . ?cited cdm:resource_legal_id_celex ?citedCelex }
  OPTIONAL { ?amender cdm:resource_legal_amends_resource_legal ?work . ?amender cdm:resource_legal_id_celex ?amenderCelex }
} LIMIT 500`, celex)
}

// distinctValues collects the sorted, deduped, non-empty values for a variable
// across the cartesian result rows.
func distinctValues(rows []map[string]string, key string) []string {
	set := make(map[string]bool)
	for _, r := range rows {
		if v := strings.TrimSpace(r[key]); v != "" {
			set[v] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// upperCELEX uppercases, dedupes and sorts a CELEX id list (CELEX case must be
// consistent so object keys and edge targets always line up).
func upperCELEX(vals []string) []string {
	set := make(map[string]bool)
	for _, v := range vals {
		if u := strings.ToUpper(strings.TrimSpace(v)); u != "" {
			set[u] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// eurovocSuffix extracts the numeric id from a eurovoc URI
// (http://eurovoc.europa.eu/2470 -> "2470").
func eurovocSuffix(uri string) string {
	s := strings.TrimRight(uri, "/")
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// collapseDirectiveRows collapses a cartesian SPARQL result set (the all-metadata
// query returns one row per multivalued-attribute combination) into a single
// EUDirective. Multivalued fields are deduped + sorted; scalar fields take the
// first sorted distinct value. CELEX ids are uppercase-normalised.
func collapseDirectiveRows(rows []map[string]string) *EUDirective {
	d := &EUDirective{}
	if v := distinctValues(rows, "title"); len(v) > 0 {
		d.FullTitle = v[0]
	}
	if v := distinctValues(rows, "dateDocument"); len(v) > 0 {
		d.DateOfDocument = v[0]
	}
	if v := distinctValues(rows, "dateEffect"); len(v) > 0 {
		d.DateOfEffect = v[0]
	}
	if v := distinctValues(rows, "resourceTypeLabel"); len(v) > 0 {
		d.Form = v[0]
	}
	if v := distinctValues(rows, "dgLabel"); len(v) > 0 {
		d.ResponsibleDG = v[0]
	}
	if v := distinctValues(rows, "directoryLabel"); len(v) > 0 {
		d.DirectoryCode = v[0]
	}
	if v := distinctValues(rows, "procedureRef"); len(v) > 0 {
		d.ProcedureNum = v[0]
	}
	if v := distinctValues(rows, "docId"); len(v) > 0 {
		d.OJReference = v[0] // raw oj: id; decoded later
	}
	d.Author = distinctValues(rows, "authorLabel")
	d.SubjectMatter = distinctValues(rows, "subjectLabel")
	d.LegalBasis = upperCELEX(distinctValues(rows, "legalBasisCelex"))
	d.CitedCELEX = upperCELEX(distinctValues(rows, "citedCelex"))
	d.ModifiedByCELEX = upperCELEX(distinctValues(rows, "amenderCelex"))
	for _, uri := range distinctValues(rows, "eurovoc") {
		d.EuroVocIDs = append(d.EuroVocIDs, eurovocSuffix(uri))
	}
	sort.Strings(d.EuroVocIDs)
	return d
}

// fetchDirectiveMeta runs the all-metadata query for a CELEX id and collapses the
// result. Returns nil when the query yields nothing (act not in CELLAR).
func fetchDirectiveMeta(ctx context.Context, celex string) (*EUDirective, error) {
	rows, err := sparqlQuery(ctx, directiveMetaQuery(celex))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return collapseDirectiveRows(rows), nil
}

// ojRefFromDocID decodes an oj: document id ("oj:JOL_2003_041_R_0026_01") into
// (series, issue, year, page). The date is NOT encoded in the id, so callers
// combine it with the OJ issue's publication date.
func ojRefFromDocID(docID string) (series, issue, year, page string, ok bool) {
	s := strings.TrimPrefix(docID, "oj:")
	parts := strings.Split(s, "_")
	if len(parts) < 5 || !strings.HasPrefix(parts[0], "JO") {
		return "", "", "", "", false
	}
	return parts[0][2:], strings.TrimLeft(parts[2], "0"), parts[1], strings.TrimLeft(parts[4], "0"), true
}

// ojIssueQuery fetches the OJ issue number/year/series/date for a CELEX id.
func ojIssueQuery(celex string) string {
	return fmt.Sprintf(`PREFIX cdm: <http://publications.europa.eu/ontology/cdm#>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
SELECT ?number ?year ?collection ?date WHERE {
  ?work cdm:resource_legal_id_celex %q^^xsd:string .
  ?work cdm:resource_legal_published_in_official-journal ?oj .
  OPTIONAL { ?oj cdm:official-journal_number ?number }
  OPTIONAL { ?oj cdm:official-journal_year ?year }
  OPTIONAL { ?oj cdm:official-journal_part_of_collection_document ?collection }
  OPTIONAL { ?oj cdm:publication_general_date_publication ?date }
} LIMIT 10`, celex)
}

// fmtOJDate renders "2003-02-14" as "14.2.2003".
func fmtOJDate(iso string) string {
	parts := strings.Split(iso, "-")
	if len(parts) != 3 {
		return iso
	}
	return fmt.Sprintf("%s.%s.%s", strings.TrimLeft(parts[2], "0"), strings.TrimLeft(parts[1], "0"), parts[0])
}

// assembleOJReference combines the decoded oj: id (series/issue/year/page) with
// the OJ issue publication date into "OJ L 41, 14.2.2003, p. 26".
func assembleOJReference(docID, ojDate string) string {
	series, issue, year, page, ok := ojRefFromDocID(docID)
	if !ok {
		return ""
	}
	if issue == "" {
		issue = year
	}
	ref := fmt.Sprintf("OJ %s %s, %s, p. %s", series, issue, year, page)
	if ojDate != "" {
		ref = fmt.Sprintf("OJ %s %s, %s, p. %s", series, issue, fmtOJDate(ojDate), page)
	}
	return ref
}

// fetchOJReference returns a human-readable OJ reference for a CELEX id, using
// the all-metadata docId for the page and a follow-up query for the date.
func fetchOJReference(ctx context.Context, celex, docID string) string {
	if docID == "" {
		return ""
	}
	rows, err := sparqlQuery(ctx, ojIssueQuery(celex))
	if err != nil || len(rows) == 0 {
		return assembleOJReference(docID, "")
	}
	date := distinctValues(rows, "date")
	ojDate := ""
	if len(date) > 0 {
		ojDate = date[0]
	}
	return assembleOJReference(docID, ojDate)
}
