package lovcheck

import (
	"regexp"
	"strconv"
	"time"
)

// dateProperties is the set of `type: date` property names declared in
// schemas/norwegian-law.yaml. These are the only fields V7 inspects:
// Law/Regulation carry date_in_force, last_change_in_force and
// date_of_publication; EUDirective declares date_of_document and date_of_effect.
// Raw-text companion fields (date_in_force_raw, date_of_publication_raw),
// year_in_force (number) and decade_in_force (string) are NOT date-typed and are
// deliberately ignored.
var dateProperties = []string{
	"date_in_force",
	"last_change_in_force",
	"date_of_publication",
	"date_of_document",
	"date_of_effect",
}

var dateRE = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)

// classifyDate reports whether a value is a well-formed YYYY-MM-DD date whose
// year is not beyond maxYear (typically time.Now().Year()+5). It returns the
// rejection reason when invalid.
func classifyDate(s string, maxYear int) (ok bool, reason string) {
	m := dateRE.FindStringSubmatch(s)
	if m == nil {
		return false, "not YYYY-MM-DD"
	}
	// time.Parse validates real calendar dates (month 1-12, day in range,
	// leap years), rejecting e.g. "2023-02-30".
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return false, "invalid calendar date"
	}
	year, _ := strconv.Atoi(m[1])
	if year > maxYear {
		return false, "year beyond threshold"
	}
	return true, ""
}
