// Command loveval builds the Norwegian-law evaluation dataset from public
// previous-exam archives (currently UiO), extracts statutory references from
// sensorveiledninger, validates them against the committed seed, and emits
// evaluation items as JSONL.
//
// Subcommands:
//
//	loveval fetch --source uio [--cache-dir evals/cache] [--limit N]
//	loveval build --cache-dir evals/cache --seed seed --out evals/dataset/uio.jsonl [--limit N]
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "fetch":
		code = runFetch(os.Args[2:])
	case "build":
		code = runBuild(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		code = 0
	default:
		fmt.Fprintf(os.Stderr, "loveval: unknown command %q\n", os.Args[1])
		usage()
		code = 2
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: loveval <command> [options]

commands:
  fetch   crawl a source archive and cache exam documents
  build   extract, pair, and validate documents into evaluation items

run "loveval <command> -h" for details.
`)
}
