// Command lovcheck validates the referential integrity of the committed seed
// corpus, fully offline.
//
// Subcommands:
//
//	lovcheck check --seed <dir> [--manifest <path>] [--json] [--max-violations N] [--samples K]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcheck"
)

// out and errOut are package-level writers so tests can capture CLI output.
var (
	out    io.Writer = os.Stdout
	errOut io.Writer = os.Stderr
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "check":
		code = runCheck(os.Args[2:])
	default:
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprintf(errOut, `usage: lovcheck <command> [options]

commands:
  check   validate the referential integrity of a seed corpus

run "lovcheck check -h" for details.
`)
}

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	seed := fs.String("seed", "seed", "path to the seed directory (contains objects/ and relationships/)")
	manifest := fs.String("manifest", "", "path to seed/manifest.json (default: <seed>/manifest.json)")
	schema := fs.String("schema", "schemas/norwegian-law.yaml", "path to the template-pack YAML for declared-set detection")
	jsonOut := fs.Bool("json", false, "emit the report as JSON")
	maxViolations := fs.Int("max-violations", lovcheck.DefaultMaxViolations, "exit non-zero when violations exceed this many (default: report-only)")
	samples := fs.Int("samples", 5, "number of sample violations to print per check")
	_ = fs.Parse(args)

	report, err := lovcheck.Check(lovcheck.Options{
		SeedDir:  *seed,
		Manifest: *manifest,
		Schema:   *schema,
		Samples:  *samples,
	})
	if err != nil {
		fmt.Fprintln(errOut, "lovcheck check:", err)
		return 1
	}

	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printReport(report)
	}

	if report.TotalViolations > *maxViolations {
		return 1
	}
	return 0
}

func printReport(r lovcheck.Report) {
	fmt.Fprintf(out, "inventory:\n")
	fmt.Fprintf(out, "  objects by type:\n")
	for _, t := range sortedKeys(r.Inventory.ObjectCountByType) {
		fmt.Fprintf(out, "    %-16s %d\n", t, r.Inventory.ObjectCountByType[t])
	}
	fmt.Fprintf(out, "  relationships by type:\n")
	for _, t := range sortedKeys(r.Inventory.RelationshipCountByType) {
		fmt.Fprintf(out, "    %-16s %d\n", t, r.Inventory.RelationshipCountByType[t])
	}
	if len(r.Inventory.DeclaredButUnusedRelationships) > 0 {
		fmt.Fprintf(out, "  declared-but-unused relationships: %s\n", joinQuoted(r.Inventory.DeclaredButUnusedRelationships))
	}
	if len(r.Inventory.DeclaredButAbsentObjectTypes) > 0 {
		fmt.Fprintf(out, "  declared-but-absent object types:  %s\n", joinQuoted(r.Inventory.DeclaredButAbsentObjectTypes))
	}

	fmt.Fprintf(out, "checks:\n")
	for _, c := range r.Checks {
		fmt.Fprintf(out, "  %s %-40s %d\n", c.ID, c.Name, c.Count)
		for _, s := range c.Samples {
			if s.Key != "" {
				fmt.Fprintf(out, "      [%s] %s (%s)\n", s.Check, s.Detail, s.Key)
			} else {
				fmt.Fprintf(out, "      [%s] %s\n", s.Check, s.Detail)
			}
		}
	}

	if len(r.Notes) > 0 {
		fmt.Fprintf(out, "notes:\n")
		for _, n := range r.Notes {
			fmt.Fprintf(out, "  - %s\n", n)
		}
	}

	fmt.Fprintf(out, "total violations: %d\n", r.TotalViolations)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func joinQuoted(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
