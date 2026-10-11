// Command lovevalgen generates evaluation items from the committed seed corpus
// and from public reference datasets.
//
// Subcommands:
//
//	lovevalgen synthetic --seed seed --out evals/generated/synthetic.jsonl [--limit N --unanswerable-ratio 0.2 --random-seed 42]
//	lovevalgen retrieval --seed seed --out evals/generated/retrieval.jsonl [--limit N --random-seed 42]
//	lovevalgen nordercase --out evals/generated/nor-casehold.jsonl [--limit N]
//
// synthetic and retrieval are corpus-derived (Norwegian statutes/regulations)
// and every emitted gold_ref is validated against the seed via internal/lovcite.
// nordercase downloads Nor-CaseHOLD (CC-BY-4.0), which is *case law* (Høyesterett
// and Skatteetaten BFU) and therefore NOT in the statute/regulation graph; its
// items are marked out_of_graph:true so the harness excludes them from the main
// score.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evalgen"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "synthetic":
		code = runSynthetic(os.Args[2:])
	case "retrieval":
		code = runRetrieval(os.Args[2:])
	case "nordercase":
		code = runNordercase(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		code = 0
	default:
		fmt.Fprintf(os.Stderr, "lovevalgen: unknown command %q\n", os.Args[1])
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: lovevalgen <command> [options]

commands:
  synthetic   Q&A/MCQ items from the seed corpus (some unanswerable)
  retrieval   corpus-derived retrieval items (task:"retrieval")
  nordercase  thin adapter over Nor-CaseHOLD (out_of_graph:true)

run "lovevalgen <command> -h" for details.
`)
}

func runSynthetic(args []string) int {
	fs := flag.NewFlagSet("synthetic", flag.ExitOnError)
	seedDir := fs.String("seed", "seed", "seed directory (objects/ + relationships/)")
	out := fs.String("out", "evals/generated/synthetic.jsonl", "output JSONL path")
	limit := fs.Int("limit", 0, "total items to emit (0 = all resolvable paragraphs)")
	ratio := fs.Float64("unanswerable-ratio", 0.2, "fraction of items that are unanswerable (0..1)")
	seed := fs.Int64("random-seed", 42, "determinism seed")
	fs.Parse(args)

	res, err := evalgen.GenerateSynthetic(evalgen.SyntheticConfig{
		SeedDir:           *seedDir,
		Out:               *out,
		Limit:             *limit,
		UnanswerableRatio: *ratio,
		RandomSeed:        *seed,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lovevalgen synthetic: %v\n", err)
		return 1
	}
	fmt.Printf("synthetic: wrote %d items to %s\n", len(res.Items), *out)
	fmt.Printf("  answerable=%d unanswerable=%d skipped=%d resolved_refs=%d total_paras=%d\n",
		res.Answerable, res.Unanswerable, res.Skipped, res.ResolvedRefs, res.TotalParas)
	return 0
}

func runRetrieval(args []string) int {
	fs := flag.NewFlagSet("retrieval", flag.ExitOnError)
	seedDir := fs.String("seed", "seed", "seed directory (objects/ + relationships/)")
	out := fs.String("out", "evals/generated/retrieval.jsonl", "output JSONL path")
	limit := fs.Int("limit", 0, "items to emit (0 = all resolvable paragraphs)")
	seed := fs.Int64("random-seed", 42, "determinism seed")
	fs.Parse(args)

	res, err := evalgen.GenerateRetrieval(evalgen.RetrievalConfig{
		SeedDir:    *seedDir,
		Out:        *out,
		Limit:      *limit,
		RandomSeed: *seed,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lovevalgen retrieval: %v\n", err)
		return 1
	}
	fmt.Printf("retrieval: wrote %d items to %s\n", res.Count, *out)
	fmt.Printf("  resolved_refs=%d skipped=%d total_paras=%d\n", res.ResolvedRefs, res.Skipped, res.TotalParas)
	return 0
}

func runNordercase(args []string) int {
	fs := flag.NewFlagSet("nordercase", flag.ExitOnError)
	out := fs.String("out", "evals/generated/nor-casehold.jsonl", "output JSONL path")
	limit := fs.Int("limit", 0, "max rows to map (0 = all)")
	url := fs.String("url", "", "dataset URL (defaults to the Nor-CaseHOLD test split)")
	fs.Parse(args)

	res, err := evalgen.GenerateNorCaseHOLD(evalgen.NorCaseHOLDConfig{
		Out:   *out,
		URL:   *url,
		Limit: *limit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lovevalgen nordercase: %v\n", err)
		return 1
	}
	fmt.Printf("nordercase: wrote %d items to %s (all out_of_graph:true)\n", res.Count, *out)
	return 0
}
