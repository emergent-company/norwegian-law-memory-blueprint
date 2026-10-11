package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ─── Amendment + version model ────────────────────────────────────────────────
//
// The versioning model derives statutory change records from two sources:
//   - law-level metadata (LastChangedByRef / AmendsRefs) — guaranteed data;
//   - "Endret ved lov …" notes in consolidated body text — essentially absent
//     in the current corpus, kept as a bounded, pure parser.
//
// All helpers are pure (no I/O) so they can be unit-tested offline.

// amendmentRecord is a derived statutory change record, the seed model for an
// Amendment object.
type amendmentRecord struct {
	AmendedByRefID     string // LOV ref of the amending act
	AmendmentDate      string // YYYY-MM-DD (empty when unknown)
	AmendmentType      string // endret|ny_lydelse|tilfoyd|opphevet|ikrafttredelse|unknown
	TargetProvisionRef string // human-readable § label (empty when law-level)
	TargetSectionID    string // structural section id (empty when law-level)
	TargetLawRefID     string // LOV ref of the amended act
	ChangeSummary      string // note text (body_note) or "" (metadata)
	Source             string // metadata|body_note|amending_section
}

// amendmentNoteKind maps the leading verb of a Norwegian consolidated-text
// amendment note to its amendment_type.
func amendmentNoteKind(lower string) string {
	switch {
	case strings.Contains(lower, "endret ved lov"), strings.Contains(lower, "endra ved lov"):
		return "endret"
	case strings.Contains(lower, "opphevet ved lov"):
		return "opphevet"
	case strings.Contains(lower, "tilføyd ved lov"), strings.Contains(lower, "tilføyd"):
		return "tilfoyd"
	case strings.Contains(lower, "ny lydelse"), strings.Contains(lower, "ny ordlyd"):
		return "ny_lydelse"
	}
	return ""
}

// amendmentDateRE matches "D. MMM YYYY nr. N" inside an amendment note. The
// month is matched in both full (januar) and abbreviated (jan/des) forms.
var amendmentDateRE = regexp.MustCompile(`(?i)(\d{1,2})\.?\s+(januar|februar|mars|april|mai|juni|juli|august|september|oktober|november|desember|jan|feb|mar|apr|jun|jul|aug|sep|okt|nov|des)\s+(\d{4})\s+nr\.?\s*(\d+)`)

// amendmentNoteTextRE matches a full amendment note for change_summary capture.
var amendmentNoteTextRE = regexp.MustCompile(`(?i)(?:endret|endra|opphevet|tilføyd)\s+ved\s+lov[^\n.;]{0,120}|ny\s+(?:lydelse|ordlyd)[^\n.;]{0,120}`)

// norwegianMonthNum maps a Norwegian month name or its common abbreviation
// (case-insensitive) to its 1-based month number; 0 when unknown.
func norwegianMonthNum(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "januar", "jan":
		return 1
	case "februar", "feb":
		return 2
	case "mars", "mar":
		return 3
	case "april", "apr":
		return 4
	case "mai":
		return 5
	case "juni", "jun":
		return 6
	case "juli", "jul":
		return 7
	case "august", "aug":
		return 8
	case "september", "sep":
		return 9
	case "oktober", "okt":
		return 10
	case "november", "nov":
		return 11
	case "desember", "des":
		return 12
	}
	return 0
}

// parseAmendmentNote parses a Norwegian consolidated-text amendment note
// ("Endret ved lov D. MMM YYYY nr. N", "Endra …", "Opphevet …", "Tilføyd …",
// "Ny lydelse …"). It returns the amendment_type, the YYYY-MM-DD date, the
// graph-native "lov/YYYY-MM-DD-N" ref, and ok=false when no note is present.
func parseAmendmentNote(text string) (kind, date, lovRef string, ok bool) {
	kind = amendmentNoteKind(strings.ToLower(text))
	if kind == "" {
		return "", "", "", false
	}
	if m := amendmentDateRE.FindStringSubmatch(text); m != nil {
		day, _ := strconv.Atoi(m[1])
		mon := norwegianMonthNum(m[2])
		year := m[3]
		num := m[4]
		if mon >= 1 && mon <= 12 && day >= 1 && day <= 31 {
			date = fmt.Sprintf("%s-%02d-%02d", year, mon, day)
			lovRef = fmt.Sprintf("lov/%s-%02d-%02d-%s", year, mon, day, num)
		}
	}
	return kind, date, lovRef, true
}

// amendmentNoteText returns the matched amendment note text (for change_summary).
func amendmentNoteText(text string) string {
	return strings.TrimSpace(amendmentNoteTextRE.FindString(text))
}

// deriveLawAmendments derives law-level amendment records from guaranteed
// metadata: LastChangedByRef (the act that last amended this law) and
// AmendsRefs (the acts this doc amends). Records carry amendment_type "unknown"
// because Lovdata metadata states neither the change kind nor the provision.
func deriveLawAmendments(d LovDoc) []amendmentRecord {
	var out []amendmentRecord
	if d.LastChangedByRef != "" {
		out = append(out, amendmentRecord{
			AmendedByRefID: d.LastChangedByRef,
			AmendmentDate:  d.LastChangeInForce,
			AmendmentType:  "unknown",
			TargetLawRefID: d.RefID,
			Source:         "metadata",
		})
	}
	for _, ref := range d.AmendsRefs {
		out = append(out, amendmentRecord{
			AmendedByRefID: d.RefID,
			AmendmentType:  "unknown",
			TargetLawRefID: ref,
			Source:         "metadata",
		})
	}
	return out
}

// bodyNoteAmendments scans paragraph content (and the law body) for amendment
// notes and returns provision-level records (source "body_note"). The law body
// is scanned once, matching at most one note (zero in practice for the current
// corpus).
func bodyNoteAmendments(d LovDoc) []amendmentRecord {
	var out []amendmentRecord
	if kind, date, lovRef, ok := parseAmendmentNote(d.Content); ok {
		out = append(out, amendmentRecord{
			AmendedByRefID: lovRef,
			AmendmentDate:  date,
			AmendmentType:  kind,
			TargetLawRefID: d.RefID,
			ChangeSummary:  amendmentNoteText(d.Content),
			Source:         "body_note",
		})
	}
	for _, p := range d.Paragraphs {
		if p.SectionID == "" {
			continue
		}
		kind, date, lovRef, ok := parseAmendmentNote(p.Content)
		if !ok {
			continue
		}
		out = append(out, amendmentRecord{
			AmendedByRefID:     lovRef,
			AmendmentDate:      date,
			AmendmentType:      kind,
			TargetProvisionRef: paragraphSectionLabel(p.ParagraphNum, p.SectionID, p.Title),
			TargetSectionID:    p.SectionID,
			TargetLawRefID:     d.RefID,
			ChangeSummary:      amendmentNoteText(p.Content),
			Source:             "body_note",
		})
	}
	return out
}

// sanitizeKey lowercases and replaces any non-alphanumeric rune with '_', giving
// a stable, JSON- and filesystem-safe key fragment.
func sanitizeKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// amendmentKey builds a deterministic object key for an Amendment record.
func amendmentKey(targetLawRefID, amendedByRefID, targetProvisionRef, source string) string {
	return "amend_" + sanitizeKey(targetLawRefID) + "_" + sanitizeKey(amendedByRefID) + "_" + sanitizeKey(targetProvisionRef) + "_" + sanitizeKey(source)
}

// versionKey builds a deterministic object key for a ProvisionVersion record.
func versionKey(lawRefID, sectionID string) string {
	return "ver_" + sanitizeKey(lawRefID) + "_" + sanitizeKey(sectionID)
}

// amendmentName builds a human-readable display name for an Amendment record.
func amendmentName(a amendmentRecord) string {
	if a.ChangeSummary != "" {
		return a.ChangeSummary
	}
	if a.AmendmentDate != "" {
		return fmt.Sprintf("%s (%s)", a.AmendedByRefID, a.AmendmentDate)
	}
	return a.AmendedByRefID
}

// keepAmendment reports whether an amendment record should be emitted: its
// target law must be a document in the current corpus (targetRefs), and a
// body_note record must carry at least an amending ref or a date (a bare
// "Ny lydelse" match with neither is a junk match and is dropped).
func keepAmendment(a amendmentRecord, targetRefs map[string]bool) bool {
	if !targetRefs[a.TargetLawRefID] {
		return false
	}
	if a.Source == "body_note" && a.AmendedByRefID == "" && a.AmendmentDate == "" {
		return false
	}
	return true
}
