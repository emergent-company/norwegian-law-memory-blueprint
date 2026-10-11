# Evaluation dataset — data acquisition & build CLI

`loveval` crawls the UiO Faculty of Law previous-exam archive, downloads exam
papers and sensorveiledninger, extracts statutory references, validates them
against the committed seed corpus, and emits evaluation items as JSONL.

## Commands

```sh
# Crawl UiO and cache documents under evals/cache/uio/ (needs network)
go run ./cmd/loveval fetch --source uio --cache-dir evals/cache
go run ./cmd/loveval fetch --source uio --limit 10        # smoke test: cap PDFs

# Extract + pair + validate into evals/dataset/uio.jsonl (needs seed/)
go run ./cmd/loveval build --cache-dir evals/cache --seed seed --out evals/dataset/uio.jsonl
go run ./cmd/loveval build --limit 2                       # smoke test: cap items

# OCR control (see "OCR fallback" below)
go run ./cmd/loveval build --no-ocr                        # disable OCR entirely
go run ./cmd/loveval build --ocr-max-pages 20              # cap OCR pages per paper (default 40)

# Conversion via the local xberg service (see "Text conversion backends")
task eval:xberg                                            # start xberg on :8000
go run ./cmd/loveval build --converter xberg               # use xberg for all conversion
go run ./cmd/loveval build --converter xberg --xberg-url http://localhost:8000
```

`pdftotext` (poppler-utils) must be on `PATH`; the build fails with a clear
error otherwise.

## Text conversion backends

`--converter` selects how PDFs are turned into text:

- `pdftotext` (default): local `pdftotext`, with the tesseract OCR fallback below.
- `xberg`: the local xberg document-extraction service
  (`ghcr.io/xberg-io/xberg:1.3.0`, `POST /extract`). Start it with
  `task eval:xberg` (publishes `:8000`); stop with `task eval:xberg:stop`.
  Relevant flags: `--xberg-url` (default `$XBERG_SERVICE_URL` or
  `http://localhost:8000`), `--xberg-timeout-ms` (default `300000`),
  `--xberg-force-ocr`, `--xberg-ocr-lang` (default `nor`),
  `--xberg-ocr-backend` (default tesseract when a language is set).

xberg conversion is hybrid, because xberg's own PDF OCR path performs poorly on
scanned UiO papers:

1. `pdftotext` is run purely as a text-layer detector.
2. Text-layer PDF → the PDF bytes are sent to xberg `/extract`.
3. Scanned PDF → pages are rendered with `pdftoppm -r 300 -png` and the PNGs are
   sent to xberg as `image/png` in one request (xberg does the OCR on the
   rendered images). This is the path that recovers clean Norwegian text.
4. Any xberg error/empty result falls back to the local `pdftotext`+tesseract
   path; a failure never aborts the run. `xberg used:` in the summary counts
   items whose `question` came from xberg.

## OCR fallback

Recent exam papers are often scanned image-only PDFs (no text layer), so
`pdftotext` returns empty. The build auto-detects this: when `pdftotext` yields
fewer than 200 non-space characters, the paper is treated as scanned and OCR'd.

- Rendering: `pdftoppm -r 300 -png -f 1 -l <ocr-max-pages> <pdf> <prefix>` into a
  temp dir, then `tesseract <png> stdout -l nor --psm 3` per page, concatenated.
- Only the **exam paper** (the `question` field) is OCR'd; sensorveiledninger are
  text PDFs and stay on the `pdftotext` path.
- Bounded: `--ocr-max-pages` (default `40`) caps rendered pages.
- `--no-ocr` disables the fallback (leaves `question` empty for scanned papers).
- Tool availability is checked at runtime (`exec.LookPath`); if `pdftoppm` or
  `tesseract` is missing, a warning is logged and the `pdftotext` result is used
  (no hard failure). `tesseract` needs the `nor` language pack.

## Cache layout

```
evals/cache/uio/
  manifest.json                      # every fetched URL: sha256, bytes, timestamp, course/semester/kind
  <course>/<semester>/<filename>.pdf # e.g. JUS1111/v26/jus1111_eksamensoppgave_signert_v26_bokmal.pdf
```

Fetch is idempotent: a URL already cached with a matching SHA-256 is skipped.

## Output schema (JSONL, one item per line)

```json
{"id":"uio-jus1111-v26","course":"JUS1111","semester":"v26",
 "question":"<full exam paper text; empty only if extraction failed>",
 "gold_points":["<one paragraph/bullet from the sensorveiledning, trimmed>"],
 "gold_refs":["lov/2002-06-21-34#§16"],
 "legal_area":"","language":"nb",
 "source":"uio","source_url":"<oppgaver page URL>",
 "oppgave_url":"<pdf>","veiledning_url":"<pdf>",
 "difficulty":"unknown","answerable":true,"license":"uio-public",
 "needs_curation":true}
```

- `gold_points` are the sensorveiledning split into natural paragraphs / numbered
  bullets and trimmed. Content is never invented.
- `gold_refs` are every statutory reference extracted from the sensorveiledning,
  normalised to `lov/<date>#§<section>` and validated against the seed. Unresolved
  references are NOT emitted; they are logged to `evals/dataset/uio.unresolved.json`.
- `language` is `nb` (bokmål) or `nn` (nynorsk) based on the exam paper used.
- `legal_area` is a lowercase Norwegian area label (`privatrett`, `formuerett`,
  `strafferett`, `forvaltningsrett`, `rettshistorie`, `metode`, `rettsteori`,
  `statsforfatningsrett`) derived from a static course-code map in
  `internal/evaldata/legalarea.go`. Unmapped course codes leave `legal_area` empty
  (`""`) rather than guessing.
- A semester with an exam paper but no sensorveiledning still emits an item with
  `gold_points: []` and `needs_curation: true` (and `answerable: false`).

## Reference extraction

Norwegian exam text abbreviates acts (`fkjl`, `avtl`, `aml`, `tvl`, `strl`,
`skl`, `sktl`, `fvl`, …). At build time we scan `seed/objects/Law.jsonl` for
`short_title`/`name` and build an abbreviation → act-key map, then expand
abbreviations before resolving sections with `internal/lovcite`. Only refs that
resolve (act exists AND section exists) become `gold_refs`.

## Licensing

UiO material is **public but copyrighted**. It is for **internal evaluation use
only**. Do not redistribute the PDFs. `evals/cache/` and `evals/dataset/` are
gitignored; neither the downloaded PDFs nor the generated dataset is committed.

## Notes for the harness lane

1. `evals/sources.yaml` is the human-readable registry; the crawler entry points
   are mirrored in Go (`internal/evaldata.UIOSource`).
2. `gold_refs` uses the fragment form `#§16` (no space), matching
   `lovcite.ResolveRef`.
3. `legal_area` is populated for known course codes; `difficulty` and
   `needs_curation` remain placeholders for the curation lane.
