package evaldata

// courseLegalArea maps a UiO course code to a lowercase Norwegian legal-area
// label matching the curated set. Codes not present here map to "" (unknown),
// which is left for the curation lane rather than guessed.
var courseLegalArea = map[string]string{
	// 1. studieår
	"JUS1111": "privatrett", // Privatrett I (avtale-/kjøpsrett, erstatningsrett)
	"JUS1211": "privatrett", // Privatrett II
	// 2. studieår
	"JUS2111": "statsforfatningsrett", // Statsforfatningsrett
	"JUS2211": "forvaltningsrett",     // Rettskilder og metode / forvaltningsrett
	// 3. studieår
	"JUS3111": "formuerett",    // Formuerett I
	"JUS3112": "formuerett",    // Formuerett II
	"JUS3211": "strafferett",   // Strafferett
	"JUS3212": "strafferett",   // Straffeprosess
	"JUS3213": "formuerett",    // Forvaltningsrett
	"JUS3220": "rettshistorie", // Rettshistorie
	// 4. studieår
	"JUS4111": "metode",      // Metode og etikk
	"JUS4121": "rettsteori",  // Alminnelig forvaltningsrett
	"JUS4122": "rettsteori",  // Statsforfatningsrett
	"JUS4123": "rettsteori",  // Folkerett
	"JUS4211": "strafferett", // Prosess (per task spec)
	"JUS4213": "strafferett",
	// eldre-studieordninger
	"JUR1000": "privatrett",
	"JUR2000": "privatrett",
	"JUR3000": "privatrett",
	"JUR4000": "privatrett",
}

// LegalAreaForCourse returns the legal-area label for a course code, or "" when
// the code is not mapped. The value is intentionally empty (rather than
// "ukjent") for unmapped codes to keep the item schema stable.
func LegalAreaForCourse(course string) string {
	if a, ok := courseLegalArea[course]; ok {
		return a
	}
	return ""
}
