// Command lovcite validates citations and quotes against the committed seed
// corpus, fully offline.
//
// Subcommands:
//
//	lovcite audit --seed <dir> [--json] [--max-unresolved N] [--samples K]
//	lovcite validate-ref --seed <dir> <ref>
//	lovcite verify-quote --seed <dir> --ref <ref> --quote "<text>"
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/lovcite"
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
	case "audit":
		code = runAudit(os.Args[2:])
	case "validate-ref":
		code = runValidateRef(os.Args[2:])
	case "verify-quote":
		code = runVerifyQuote(os.Args[2:])
	default:
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprintf(errOut, `usage: lovcite <command> [options]

commands:
  audit          scan Law/Regulation content and report citation resolution
  validate-ref   resolve a single reference to provision(s)
  verify-quote   assert a quote is a substring of a resolved provision

run "lovcite <command> -h" for details.
`)
}

func runAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	seed := fs.String("seed", "seed", "path to the seed directory (contains objects/ and relationships/)")
	jsonOut := fs.Bool("json", false, "emit the summary as JSON")
	maxUnresolved := fs.Int("max-unresolved", lovcite.DefaultMaxUnresolved, "exit non-zero when unresolved exceeds this many (default: report-only)")
	samples := fs.Int("samples", 10, "number of sample unresolved references to print")
	_ = fs.Parse(args)

	idx, err := lovcite.Build(*seed, lovcite.BuildOptions{})
	if err != nil {
		fmt.Fprintln(errOut, "lovcite audit:", err)
		return 1
	}
	summary, err := idx.Audit(*seed, *samples)
	if err != nil {
		fmt.Fprintln(errOut, "lovcite audit:", err)
		return 1
	}

	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(summary)
	} else {
		printAudit(summary)
	}

	if summary.TotalUnresolved > *maxUnresolved {
		return 1
	}
	return 0
}

func printAudit(s lovcite.AuditSummary) {
	fmt.Fprintf(out, "scanned docs:            %d\n", s.ScannedDocs)
	fmt.Fprintf(out, "act refs (lov):          found=%d resolved=%d unresolved=%d\n",
		s.ActLovFound, s.ActLovResolved, s.ActLovFound-s.ActLovResolved)
	fmt.Fprintf(out, "act refs (forskrift):    found=%d resolved=%d unresolved=%d\n",
		s.ActForskriftFound, s.ActForskriftResolved, s.ActForskriftFound-s.ActForskriftResolved)
	fmt.Fprintf(out, "section refs:            found=%d resolved=%d unresolved=%d\n",
		s.SectionFound, s.SectionResolved, s.SectionFound-s.SectionResolved)
	fmt.Fprintf(out, "total refs:              found=%d resolved=%d unresolved=%d\n",
		s.TotalFound, s.TotalResolved, s.TotalUnresolved)
	fmt.Fprintf(out, "resolution rate:         %.2f%%\n", s.ResolutionRate*100)
	if len(s.Samples) > 0 {
		fmt.Fprintf(out, "unresolved samples (%d):\n", len(s.Samples))
		for _, sm := range s.Samples {
			fmt.Fprintf(out, "  %s:%s  %-28s %s\n", sm.File, sm.Key, sm.Ref, sm.Reason)
		}
	}
}

func runValidateRef(args []string) int {
	fs := flag.NewFlagSet("validate-ref", flag.ExitOnError)
	seed := fs.String("seed", "seed", "path to the seed directory")
	_ = fs.Parse(args)
	ref := strings.TrimSpace(fs.Arg(0))
	if ref == "" {
		fmt.Fprintln(errOut, "validate-ref: missing <ref>")
		fs.Usage()
		return 2
	}

	idx, err := lovcite.Build(*seed, lovcite.BuildOptions{})
	if err != nil {
		fmt.Fprintln(errOut, "lovcite validate-ref:", err)
		return 1
	}

	res := idx.ResolveRef(ref)
	fmt.Fprintf(out, "ref: %s\n", ref)
	if !res.ActFound {
		fmt.Fprintf(out, "not found: unknown act %q\n", res.Base)
		return 1
	}
	if len(res.Targets) == 0 {
		fmt.Fprintf(out, "not found: no provision for %q\n", ref)
		return 1
	}
	fmt.Fprintf(out, "resolved to (%d):\n", len(res.Targets))
	for _, t := range res.Targets {
		confirmed := ""
		if !idx.HasChild(t.LawRefID, t.SectionID) {
			confirmed = "  [no HAS_PARAGRAPH link]"
		}
		fmt.Fprintf(out, "  %s  paragraph_num=%q section_id=%q%s\n",
			t.Key, t.ParagraphNum, t.SectionID, confirmed)
	}
	return 0
}

func runVerifyQuote(args []string) int {
	fs := flag.NewFlagSet("verify-quote", flag.ExitOnError)
	seed := fs.String("seed", "seed", "path to the seed directory")
	ref := fs.String("ref", "", "reference to resolve, e.g. lov/2005-06-17-62#§1-1")
	quote := fs.String("quote", "", "quote text to verify")
	_ = fs.Parse(args)
	if *ref == "" {
		fmt.Fprintln(errOut, "verify-quote: --ref is required")
		return 2
	}
	if *quote == "" {
		fmt.Fprintln(errOut, "verify-quote: --quote is required")
		return 2
	}

	idx, err := lovcite.Build(*seed, lovcite.BuildOptions{RetainContent: true})
	if err != nil {
		fmt.Fprintln(errOut, "lovcite verify-quote:", err)
		return 1
	}

	res := idx.VerifyQuote(*ref, *quote)
	fmt.Fprintf(out, "ref: %s\n", *ref)
	fmt.Fprintf(out, "quote: %s\n", *quote)
	if res.Matched {
		p := res.Paragraph
		fmt.Fprintf(out, "MATCH: %s (paragraph_num=%q section_id=%q)\n", p.Key, p.ParagraphNum, p.SectionID)
		fmt.Fprintf(out, "context: %s\n", res.Excerpt)
		return 0
	}
	fmt.Fprintln(out, "NO MATCH")
	if res.Closest != nil {
		fmt.Fprintf(out, "closest: %s (paragraph_num=%q section_id=%q)\n",
			res.Closest.Key, res.Closest.ParagraphNum, res.Closest.SectionID)
		fmt.Fprintf(out, "context: %s\n", res.Excerpt)
	} else {
		fmt.Fprintf(out, "not found: %q did not resolve to any provision\n", *ref)
	}
	return 1
}
