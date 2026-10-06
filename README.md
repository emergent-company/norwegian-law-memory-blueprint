# Norwegian Law Memory Blueprint

A standalone [Memory](https://emergent-company.ai) blueprint that recreates the
**Norwegian Law Assistant** — a knowledge graph of the full Norwegian legal
corpus plus an agent that answers cited, source-grounded questions about
Norwegian law.

Apply it to any Memory project to get the schema, the agent, and the ingested
legal corpus in one step.

## What it creates

### Agent

| Agent | Description |
|---|---|
| `norwegian-law-assistant` | Answers questions about Norwegian statutes, regulations, provisions, ministries, legal areas and EU/EEA directives. Always searches the graph and cites the act (`short_title` + `ref_id`) and section. Answers in Norwegian or English. |

### Object types (7)

| Type | Description |
|---|---|
| `Law` | Norwegian acts of parliament (`lov/…`) |
| `Regulation` | Central regulations (`forskrift/…`) |
| `Ministry` | Government ministries that administer laws |
| `LegalArea` | Top-level and sub-level legal classification areas |
| `LegalParagraph` | Individual sections/articles within a law or regulation |
| `EUDirective` | EU/EEA directives referenced by Norwegian law |
| `EuroVocConcept` | EuroVoc thesaurus concepts attached to EU directives |

### Relationship types (13)

| Relationship | Meaning |
|---|---|
| `ADMINISTERED_BY` | Law/Regulation → Ministry |
| `IN_LEGAL_AREA` | Law/Regulation → LegalArea |
| `AMENDED_BY` | Law → the act that last amended it |
| `AMENDS` | Law → acts it amends |
| `SEE_ALSO` | Non-hierarchical cross-reference |
| `HAS_LANGUAGE_VARIANT` | Bokmål ↔ Nynorsk variants of the same document |
| `REFERENCES` | Body hyperlink cross-reference |
| `IMPLEMENTS_EEA` | Law/Regulation → EU directive it implements |
| `HAS_PARAGRAPH` | Law/Regulation → LegalParagraph |
| `CITES_EU_LAW` | Law body inline citation → EU directive |
| `EU_CITES` | EUDirective → other EU instruments it cites |
| `EU_MODIFIED_BY` | EUDirective → amending instruments |
| `HAS_EUROVOC_DESCRIPTOR` | EUDirective → EuroVoc concept |

## Repository layout

```
norwegian-law-memory-blueprint/
├── schemas/
│   └── norwegian-law.yaml          # Memory template pack (object + relationship types)
├── agents/
│   └── norwegian-law-assistant.yaml # Norwegian Law Assistant agent definition
├── seed/
│   ├── objects/                     # per-type JSONL objects
│   └── relationships/               # per-type JSONL relationships
├── internal/
│   └── lovcite/                     # citation index + resolver + verifier (unit-tested)
└── cmd/
    ├── seeder/
    │   └── main.go                  # standalone Lovdata seeder + seed exporter
    └── lovcite/
        └── main.go                  # offline citation/quote verification CLI
```

A blueprint is applied with the Memory CLI, which loads `schemas/`, `agents/`,
`seed/objects/` and `seed/relationships/` in that order.

## Apply the blueprint

```bash
# Preview (no writes)
memory blueprints inspect ./norwegian-law-memory-blueprint

# Offline validation (no API calls)
memory blueprints validate ./norwegian-law-memory-blueprint

# Apply to a project
memory blueprints install ./norwegian-law-memory-blueprint --project <project>
```

> Note: the command is `memory blueprints install` (older docs said `apply`).

Applying the blueprint creates the schema pack, the agent, and the seed graph.
Seed application is idempotent by object `key`: existing keys are skipped unless
`--upgrade` is passed.

## Seed data

`seed/` contains the full parsed corpus as portable JSONL — no server required
to produce or apply it:

- object line: `{"type":"Law","key":"lov/1687-04-15","properties":{…}}`
- relationship line: `{"type":"HAS_PARAGRAPH","srcKey":"lov/…","dstKey":"lov/…#…","properties":{…}}`

The bundle is roughly **144k objects / 165k relationships** (laws, regulations,
provisions, ministries, legal areas and their relationships). `LegalParagraph`
and the relationship files dominate the size. Large types are split at 50 MB
into `<Type>.001.jsonl`, `<Type>.002.jsonl`, … (the Memory CLI's blueprint
loader reassembles them transparently).

### `LegalParagraph` section labels

Every `LegalParagraph` carries a first-class, human-readable `section_label`, so
consumers never have to reverse-engineer the `section_id` key:

- `paragraph_num` — the statutory paragraph number as printed when the source
  carried a `legalArticleValue` header (e.g. `§ 8-10`, `§ 121 d`).
- `section_label` — `paragraph_num` when present (`§ 8-10`); otherwise a
  structural position derived from the section id at ingestion
  (e.g. `Kapittel 1, ledd 1`). It is never the raw `section_id` and is never
  empty for a paragraph with a section id.
- `name` — `paragraph_num` + optional title (`§ 8-10 Finansiell bistand`), or
  the `section_label` when there is no § header.

`section_id` remains the load-bearing join key (e.g.
`kapittel-3-paragraf-4`) and is unchanged. Note that its `kapittel`/`paragraf`
numbers are Lovdata **HTML structural indices, not statutory numbers** — e.g.
`lov/1915-08-13-5#kapittel-8-paragraf-4` is statutory `§ 121 d` — so the seeder
never derives a § from the key.

Alternatively, `memory blueprints dump <output-dir>` exports any already-populated
project back into the same `seed/objects/*.jsonl` + `seed/relationships/*.jsonl`
format.

## Data sources

| Source | License |
|---|---|
| [Lovdata public datasets](https://lovdata.no/info/utviklerinfo) (`gjeldende-lover.tar.bz2`, `gjeldende-sentrale-forskrifter.tar.bz2`) | NLOD 2.0 |
| [EUR-Lex](https://eur-lex.europa.eu) directive metadata | Public |
| [Publications Office EuroVoc SPARQL](https://publications.europa.eu/webapi/rdf/sparql) | Public |
| [Stortinget data API](https://data.stortinget.no) (forarbeider full text) | NLOD 2.0 |

## Regenerating the corpus

The seeder runs in two modes.

### Export a portable seed (no Memory server)

```bash
go build ./cmd/seeder/
./seeder --dump-seed . --dataset both
```

Downloads and parses the Lovdata archives, optionally enriches EU directive
metadata, and writes `seed/objects/*.jsonl` + `seed/relationships/*.jsonl`.
Fully offline with respect to Memory — no token, project or server needed.

Useful flags: `--dataset laws|regulations|both`, `--limit N`, `--skip-eu`,
`--cache-dir <dir>`, `--ingest-only`, `--download-only` (download + parse to
cache, skip export).

### Ingest directly into a Memory project

```bash
./seeder \
  --server http://localhost:3012 \
  --token  <project-api-token>
```

The target project is resolved by name (`--project-name`, default
`"Norwegian Law"`): the seeder finds an existing project of that name in the
org (`--org-id`, or the user's accessible projects when unset) and creates it if
missing. Pass `--project <id>` to skip the lookup and target a specific project.

Two-phase, resumable ingestion (objects, then relationships) using the Memory
SDK: batch size 100, 20 concurrent workers, checkpointing in `--state-dir`
(`state.json`, `idmap.json`, `rels_failed.jsonl`). Re-run the same command to
resume after an interruption.

Smoke test (cached data, no EU enrichment):

```bash
./seeder --server <url> --token <token> --project <id> \
  --ingest-only --limit 50 --skip-eu
```

## All seeder flags

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--server` | `MEMORY_SERVER` | — | Memory server URL (required unless `--dump-seed`) |
| `--token` | `MEMORY_PROJECT_TOKEN` | — | Project API token (required unless `--dump-seed`) |
| `--project` | `MEMORY_PROJECT_ID` | — | Project ID (optional; overrides name-based auto-resolve) |
| `--org-id` | `MEMORY_ORG_ID` | — | Organisation ID for project creation (default: user's org) |
| `--project-name` | `NORWEGIAN_LAW_PROJECT_NAME` | `Norwegian Law` | Project name to find or create |
| `--dump-seed` | `SEED_DUMP_DIR` | — | Export portable seed JSONL to `<dir>/seed/` instead of uploading |
| `--state-dir` | `MEMORY_STATE_DIR` | `~/.norwegian-law-seed-state` | Checkpoint directory |
| `--cache-dir` | `LOVDATA_CACHE_DIR` | `/tmp/lovdata_data` | Download cache |
| `--limit` | `SEED_LIMIT` | `0` (all) | Max documents per dataset |
| `--skip-eu` | — | false | Skip EUR-Lex / EuroVoc enrichment |
| `--eu-limit` | — | `0` (all) | Max EU directives to fetch |
| `--workers` | — | `20` | Parallel upload workers |
| `--batch` | — | `100` | Bulk API batch size |
| `--ingest-only` | — | false | Skip download; use cached archive |
| `--download-only` | — | false | Download + parse to cache, skip ingestion |
| `--cleanup` | — | false | Delete the target project after the run |
| `--dataset` | — | `both` | `laws`, `regulations`, or `both` |

## Reconciling a live project (`seedfill`)

`cmd/seedfill` reconciles an already-ingested project against the committed seed
data, creating only the missing objects/relationships (useful after a partial
ingest). It can also re-coerce date properties into the server's canonical
RFC3339 form after the schema pack changed them from `string` to `date`.

```bash
go build ./cmd/seedfill/

# Diff + fill gaps (dry-run previews only)
./seedfill --server <url> --token <token> --project <id> --dir . --dry-run

# Coerce existing objects' date fields (Law/Regulation/EUDirective) to RFC3339
./seedfill --server <url> --token <token> --project <id> --dir . --retype-dates

# Same, but via object upsert (full property replace) — use when by-id graph
# writes (PATCH /objects/{id}, /objects/bulk-update) are broken on the server
./seedfill --server <url> --token <token> --project <id> --dir . --retype-via-upsert

# Both passes
./seedfill --server <url> --token <token> --project <id> --dir . --retype-and-fill

# Sync seed properties onto objects that already exist (PATCH by id, merge only
# changed keys), skipping gap-filling
./seedfill --server <url> --token <token> --project <id> --dir . --sync-props

# Sync seed properties, then fill missing objects + relationships
./seedfill --server <url> --token <token> --project <id> --dir . --sync-props-and-fill
```

`--retype-dates` is idempotent — a second run reports zero patches — and by
default skips gap-filling unless `--retype-and-fill` is also set. Respects
`--dry-run`, `--batch` (max 100), `--workers` (default 4), `--page-size`
(default 250, max 1000; the object/relationship enumeration page size) and
`--http-timeout` (default `60s`; the HTTP client timeout, lowered from 5m so
retries cycle faster during dev-server flaps). Env fallbacks: `MEMORY_SERVER`,
`MEMORY_PROJECT_TOKEN`, `MEMORY_PROJECT_ID`, `SEED_DIR`, `SEED_RETYPE_DATES`,
`SEEDFILL_PAGE_SIZE`, `SEEDFILL_HTTP_TIMEOUT`.

> **Load shedding.** Newer servers return `429 shed: system under load, request
> shed` under write pressure; `seedfill` treats `429`/`shed` as retryable with
> backoff (up to 60s) instead of failing the batch permanently. A full backfill
> is still I/O-bound, so run it gently — e.g. `--workers 1 --batch 20` — and
> expect hours for ~100k objects. Re-runs are idempotent and resumable.

`--retype-via-upsert` re-coerces the same date fields but through
`PUT /api/graph/objects/upsert` (resolved by `type`+`key`, not by id), which
still works when by-id writes are down. Upsert **replaces** the object's
properties, so it sends each object's complete seed property set and passes
through the live labels. It is likewise idempotent and skips gap-filling unless
`--retype-and-fill` is also set. Env fallback: `SEED_RETYPE_VIA_UPSERT`.

`--sync-props` PATCHes seed object properties onto objects that already exist in
the live project, matched by seed `key`. Only the changed properties are sent —
`PATCH` merges, so labels, status, assignee, and any live-only properties are
preserved — and non-date fields are compared with a canonical-JSON deep-equal
while date-typed fields reuse the same RFC3339 idempotency as `--retype-dates`.
It is idempotent (a second run reports zero patches), respects `--dry-run`, and
skips gap-filling entirely. `--sync-props-and-fill` runs the sync pass first,
then the normal object + relationship gap-fill. Env fallback for `--sync-props`:
`SEED_SYNC_PROPS`.

## Citation verification (`lovcite`)

`cmd/lovcite` is a fully offline CLI that validates citations and quotes against
the committed seed corpus. It never touches the network and produces
deterministic output (map keys are always sorted before printing).

```bash
go run ./cmd/lovcite audit --seed seed
go run ./cmd/lovcite validate-ref --seed seed 'lov/2005-06-17-62#§1-1'
go run ./cmd/lovcite verify-quote --seed seed --ref 'lov/2005-06-17-62#§1-1' --quote 'å sikre et arbeidsmiljø'
```

### `audit` — scan the corpus for unresolvable citations

```bash
lovcite audit --seed <dir> [--json] [--max-unresolved N] [--samples K]
```

Loads `objects/` (+ `relationships/` for endpoint checks) and scans the
`content` of every `Law` and `Regulation` object for two kinds of citation:

- **Explicit act refs** — `lov/…`, `forskrift/…` — resolved by checking that the
  corresponding `Law`/`Regulation` object key exists.
- **Section refs** — `§ N` / `§ N-M` — resolved within the *owning document's*
  paragraph index (`law_ref_id → paragraph_num → section_id`).

It prints totals per kind plus the overall resolution rate and up to `K` sample
unresolved references with `file:key` context. `--json` emits the same summary
as JSON. By default `--max-unresolved` is the maximum int value (report-only);
pass a number to exit non-zero when unresolved exceeds it:

```bash
# fail only if more than 1000 citations are unresolvable
lovcite audit --seed seed --max-unresolved 1000
```

### `validate-ref` — resolve a single reference

```bash
lovcite validate-ref --seed <dir> <ref>
```

Accepts an act reference, a section reference, or a section-id reference and
prints the resolved provision(s), exiting 1 when unresolved:

```bash
lovcite validate-ref --seed seed 'lov/2005-06-17-62'                      # whole act (all provisions)
lovcite validate-ref --seed seed 'lov/2005-06-17-62#§1-1'                 # by printed number
lovcite validate-ref --seed seed 'lov/2005-06-17-62#kapittel-1-paragraf-1' # by section id
```

### `verify-quote` — assert a quote is in a provision

```bash
lovcite verify-quote --seed <dir> --ref <ref> --quote "<text>"
```

Resolves `<ref>` to a provision and asserts `<quote>` is a substring after
whitespace normalisation (runs of whitespace collapse to a single space; the
match is case-sensitive). Exits 0 with the matched location on success, 1 with
the closest provision and a context excerpt on mismatch.

### Resolution rules and limitations

- **Scope.** Only `Law` and `Regulation` `content` is scanned. `LegalParagraph`
  content is not scanned separately — `Law`/`Regulation` content already embeds
  every paragraph body, so scanning both would double-count. `EUDirective`
  content uses article numbering rather than `§` and is not scanned.
- **Headings are not citations.** Markdown heading lines (`### § 1-1 — …`) are
  structural section titles and are skipped.
- **`§§` (plural) is out of scope.** `§§ 40, 41` and `§§ 6-12` denote ranges or
  lists, which the resolver does not expand; only single `§ N` / `§ N-M` refs are
  checked.
- **Number forms.** Both flat (`§ 22`) and chapterised (`§ 1-3`) numbering are
  supported; the range separator may be `-`, `–` or `—`. A letter suffix is only
  recognised when directly attached (`§ 1-3a`). A space-separated letter
  (`§ 1-1 a`) is treated as prose — a trailing single letter is ambiguous in
  Norwegian (`i` = "in", `å` = "to") — so such a ref resolves to the base
  section `§ 1-1` if present.
- **Cross-act attribution (conservative rule).** A `§` ref resolves against the
  owning document, *unless* an explicit act ref (`lov/…` / `forskrift/…`) appears
  on the same line before it — with no other `§` in between and within 80 bytes —
  in which case it resolves against that act. Human-readable act names
  (`tvisteloven § 6-10`, `Grundloven § 75`, `lov om hittegods … §§ 6-12`) are
  **not** mapped to acts (the corpus has no name→key index), so they fall back to
  the owning document and typically come out unresolved.
- **Endpoint checks.** A `#section-id` fragment is confirmed against the
  `HAS_PARAGRAPH` relationships, not only the object index.

### Known corpus gaps (honest baseline)

Running `go run ./cmd/lovcite audit --seed seed` against the committed seed
reports a **~36%** section-ref resolution rate. This is expected and is not a
parser defect. The dominant causes of unresolved refs, in order:

1. **Documents without parsed `§` numbering.** ~4,183 of 5,661 laws/regulations
   have *zero* paragraphs with a non-empty `paragraph_num` (historic acts such as
   `lov/1687-04-15` and many regulations use `ledd`/chapter numbering the seeder
   did not convert to `§ N`). Their inline `§` refs cannot be resolved. These
   rows now carry a human-readable `section_label` (e.g. `Kapittel 1, ledd 1`),
   but the label is a structural position, not a statutory §.
2. **References to non-ingested sections.** Some laws reference sections of
   themselves that are absent from the seed (e.g. `lov/1999-03-26-14`
   (skatteloven) body text cites `§ 14-41`, but chapter 14 was not ingested).
3. **Cross-act references by name** (see the conservative rule above).

Explicit act refs are rare in the corpus (four occurrences, all resolving).

## Prerequisites

- Go 1.25+
- For direct ingestion only: a running Memory server and a project API token

## End-to-end tests

`tests/e2e/` contains standalone functional tests for the ingested knowledge
graph and the `norwegian-law-assistant` agent. They run against a live Memory
server and skip automatically when no credentials are present — only
`TestAgentIsReadOnly` is offline and always runs.

```bash
export TEST_SERVER_URL="https://api.dev.emergent-company.ai"
export TEST_API_TOKEN="emt_…"

cd /root/norwegian-law-memory-blueprint
PATH="/root/go/bin:$PATH" go test ./tests/e2e/ -v -count=1 -timeout 30m
```

Or via Task:

```bash
PATH="/root/go/bin:$PATH" task e2e            # run the suite
PATH="/root/go/bin:$PATH" task e2e:install    # install blueprint first (LAW_E2E_INSTALL=1)
PATH="/root/go/bin:$PATH" task e2e:compile    # compile-only check
```

See `tests/e2e/README.md` for the full env-var reference.

## License

Source code: MIT. Data is re-distributed under its original licenses (see
Data sources above).
