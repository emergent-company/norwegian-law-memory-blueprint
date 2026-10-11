package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/emergent-company/norwegian-law-memory-blueprint/internal/evaldata"
)

func runBuild(args []string) int {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	cacheDir := fs.String("cache-dir", "evals/cache", "cache root directory")
	source := fs.String("source", "uio", "source registry key (only \"uio\" is supported)")
	seed := fs.String("seed", "seed", "path to the seed directory (contains objects/ and relationships/)")
	out := fs.String("out", "evals/dataset/uio.jsonl", "output JSONL path")
	limit := fs.Int("limit", 0, "max items to emit (0 = unlimited)")
	ocrMaxPages := fs.Int("ocr-max-pages", evaldata.DefaultOCRMaxPages, "max pages to OCR per scanned exam paper")
	noOCR := fs.Bool("no-ocr", false, "disable OCR fallback for image-only exam papers")
	converter := fs.String("converter", "pdftotext", "text converter: \"pdftotext\" or \"xberg\"")
	xbergURL := fs.String("xberg-url", xbergDefaultURL(), "xberg service base URL (default: $XBERG_SERVICE_URL or http://localhost:8000)")
	xbergTimeoutMs := fs.Int("xberg-timeout-ms", evaldata.DefaultXbergTimeoutMs, "xberg request timeout in milliseconds")
	xbergForceOCR := fs.Bool("xberg-force-ocr", false, "request forced OCR via xberg")
	xbergOCRLang := fs.String("xberg-ocr-lang", "nor", "xberg OCR language(s), \"+\"-separated (e.g. nor, nor+eng)")
	xbergOCRBackend := fs.String("xberg-ocr-backend", "", "xberg OCR backend (e.g. tesseract)")
	_ = fs.Parse(args)

	if *source != "uio" {
		fmt.Fprintf(os.Stderr, "build: unsupported source %q (only \"uio\")\n", *source)
		return 2
	}
	if *converter != "pdftotext" && *converter != "xberg" {
		fmt.Fprintf(os.Stderr, "build: unsupported converter %q (must be \"pdftotext\" or \"xberg\")\n", *converter)
		return 2
	}

	var xbergCfg *evaldata.XbergConfig
	if *converter == "xberg" {
		xbergCfg = &evaldata.XbergConfig{
			BaseURL:     *xbergURL,
			TimeoutMs:   *xbergTimeoutMs,
			ForceOCR:    *xbergForceOCR,
			OCRLanguage: *xbergOCRLang,
			OCRBackend:  *xbergOCRBackend,
		}
	}

	res, err := evaldata.BuildDataset(evaldata.BuildConfig{
		CacheDir:    *cacheDir,
		Source:      *source,
		SeedDir:     *seed,
		Out:         *out,
		Limit:       *limit,
		OCRMaxPages: *ocrMaxPages,
		NoOCR:       *noOCR,
		Converter:   *converter,
		Xberg:       xbergCfg,
		Logf:        func(f string, a ...interface{}) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		return 1
	}

	if err := evaldata.WriteItemsFile(*out, res.Items); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		return 1
	}

	unresolvedPath := unresolvedSidecar(*out)
	if err := evaldata.WriteUnresolved(unresolvedPath, res.Unresolved); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		return 1
	}

	s := res.Stats
	fmt.Printf("documents:        %d\n", s.Documents)
	fmt.Printf("exam papers:      %d\n", s.ExamPapers)
	fmt.Printf("sensorveiledning: %d\n", s.Veiledninger)
	fmt.Printf("items:            %d\n", s.Items)
	fmt.Printf("with gold points: %d\n", s.WithGoldPoints)
	fmt.Printf("no veiledning:    %d\n", s.NoVeiledning)
	fmt.Printf("converter:        %s\n", *converter)
	fmt.Printf("ocr used:         %d\n", s.OCRUsed)
	if *converter == "xberg" {
		fmt.Printf("xberg used:       %d\n", s.XbergUsed)
	}
	fmt.Printf("gold refs:        %d resolved, %d unresolved\n", s.ResolvedRefs, s.UnresolvedRefs)
	fmt.Printf("output:           %s\n", *out)
	if len(res.Unresolved) > 0 {
		fmt.Printf("unresolved:       %s\n", unresolvedPath)
	}
	return 0
}

// xbergDefaultURL returns the default xberg base URL: $XBERG_SERVICE_URL when
// set, else http://localhost:8000.
func xbergDefaultURL() string {
	if v := os.Getenv("XBERG_SERVICE_URL"); v != "" {
		return v
	}
	return evaldata.DefaultXbergBaseURL
}

// unresolvedSidecar derives evals/dataset/uio.unresolved.json from the out path.
func unresolvedSidecar(out string) string {
	ext := filepath.Ext(out)
	base := out[:len(out)-len(ext)]
	return base + ".unresolved.json"
}
