package lovcheck

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// SchemaDeclared holds the object/relationship type names declared in
// schemas/norwegian-law.yaml, extracted by cheap line scanning (no YAML
// dependency).
type SchemaDeclared struct {
	ObjectTypes       []string
	RelationshipTypes []string
}

var (
	topLevelKeyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*:\s*$`)
	itemNameRE    = regexp.MustCompile(`^\s*-\s*name:\s*(.+?)\s*$`)
)

// LoadSchemaDeclared scans a Memory template-pack YAML for the `objectTypes:`
// and `relationshipTypes:` lists and returns the declared `name` values in file
// order. It is intentionally a lightweight string scan: the template pack has a
// fixed shape (top-level keys at column 0, list entries as `  - name: X`), and
// adding a YAML parser is out of scope. The caller treats a missing/unreadable
// file as "declared set unavailable" rather than an error in the corpus.
func LoadSchemaDeclared(path string) (SchemaDeclared, error) {
	var sd SchemaDeclared
	f, err := os.Open(path)
	if err != nil {
		return sd, err
	}
	defer f.Close()

	var inObjects, inRelationships bool
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		switch {
		case line == "objectTypes:":
			inObjects, inRelationships = true, false
		case line == "relationshipTypes:":
			inObjects, inRelationships = false, true
		case topLevelKeyRE.MatchString(line):
			// Any other top-level key (name:, version:, …) ends the section.
			inObjects, inRelationships = false, false
		default:
			if m := itemNameRE.FindStringSubmatch(line); m != nil {
				name := strings.TrimSpace(m[1])
				if inObjects {
					sd.ObjectTypes = append(sd.ObjectTypes, name)
				} else if inRelationships {
					sd.RelationshipTypes = append(sd.RelationshipTypes, name)
				}
			}
		}
	}
	return sd, sc.Err()
}
