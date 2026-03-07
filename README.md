# Norwegian Law Memory Blueprint

A standalone Memory blueprint that ingests the full Norwegian legal corpus into a
[Memory](https://emergent-company.ai) knowledge-graph project.

## What it creates

**7 object types**

| Type | Description |
|---|---|
| `Law` | Norwegian laws (`lov/…`) |
| `Regulation` | Norwegian regulations (`forskrift/…`) |
| `Ministry` | Government ministries that administer laws |
| `LegalArea` | Top-level and sub-level legal classification areas |
| `LegalParagraph` | Individual sections/articles within a law or regulation |
| `EUDirective` | EU/EEA directives referenced by Norwegian law |
| `EuroVocConcept` | EuroVoc thesaurus concepts attached to EU directives |

**13 relationship types**

| Relationship | Meaning |
|---|---|
| `ADMINISTERED_BY` | Law → Ministry |
| `IN_LEGAL_AREA` | Law/Regulation → LegalArea |
| `AMENDED_BY` | Law → the law that last amended it |
| `AMENDS` | Law → laws it amends |
| `SEE_ALSO` | Cross-reference between laws |
| `HAS_LANGUAGE_VARIANT` | Norwegian ↔ Nynorsk variants of the same document |
| `REFERENCES` | Body hyperlink cross-reference |
| `IMPLEMENTS_EEA` | Law → EU directive it implements |
| `HAS_PARAGRAPH` | Law/Regulation → LegalParagraph |
| `CITES_EU_LAW` | Law body inline citation → EU directive |
| `EU_CITES` | EUDirective → other EU instruments it cites |
| `EU_MODIFIED_BY` | EUDirective → amending instruments |
| `HAS_EUROVOC_DESCRIPTOR` | EUDirective → EuroVoc concept |

## Data sources

| Source | License |
|---|---|
| [Lovdata public datasets](https://lovdata.no/info/utviklerinfo) (`gjeldende-lover.tar.bz2`, `gjeldende-sentrale-forskrifter.tar.bz2`) | NLOD 2.0 |
| [EUR-Lex](https://eur-lex.europa.eu) directive metadata | Public |
| [Publications Office EuroVoc SPARQL](https://publications.europa.eu/webapi/rdf/sparql) | Public |

## Repository layout

```
norwegian-law-memory-blueprint/
├── packs/
│   └── norwegian-law.yaml   # Memory template pack (object + relationship types)
└── cmd/
    └── seeder/
        └── main.go          # Standalone Go seeder (~2 000 lines)
```

## Prerequisites

- Go 1.24+
- A running [Memory](https://emergent-company.ai) server
- A Memory project with the template pack installed

## Install the template pack

```bash
memory blueprints apply /path/to/norwegian-law-memory-blueprint --server http://localhost:3012
```

Or via the admin UI: **Settings → Template Packs → Import**.

## Build & run the seeder

```bash
go build ./cmd/seeder/
```

### Full ingest (downloads ~800 MB of Lovdata archives)

```bash
./seeder \
  --server  http://localhost:3012 \
  --token   <project-api-token> \
  --project <project-id>
```

### Smoke test (50 docs, no EU enrichment, uses cached JSON if present)

```bash
./seeder \
  --server     http://localhost:3012 \
  --token      <token> \
  --project    <id> \
  --ingest-only \
  --limit      50 \
  --skip-eu
```

## All flags

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--server` | `MEMORY_SERVER` | — | Memory server URL (required) |
| `--token` | `MEMORY_PROJECT_TOKEN` | — | Project API token (required) |
| `--project` | `MEMORY_PROJECT_ID` | — | Project ID (required) |
| `--state-dir` | `MEMORY_STATE_DIR` | `~/.norwegian-law-seed-state` | Checkpoint directory |
| `--cache-dir` | `LOVDATA_CACHE_DIR` | `/tmp/lovdata_data` | Downloaded archive cache |
| `--limit` | `SEED_LIMIT` | `0` (no limit) | Max documents per dataset |
| `--skip-eu` | — | false | Skip EUR-Lex / EuroVoc enrichment |
| `--eu-limit` | — | `0` (all) | Max EU directives to fetch |
| `--workers` | — | `20` | Parallel upload workers |
| `--batch` | — | `100` | Batch size for bulk API calls |
| `--ingest-only` | — | false | Skip download; use cached JSON |
| `--dataset` | — | `both` | `laws`, `regulations`, or `both` |

## Resumable ingestion

The seeder persists state in `--state-dir`:

- `state.json` — current phase (`objects_pending` → `objects_done` → `rels_pending` → `done`)
- `idmap.json` — key→UUID mapping (written after all objects are uploaded)
- `rels_done.txt` — batch indices already uploaded (for relationship resume)
- `rels_failed.jsonl` — batches that failed permanently

If interrupted, re-run with the same flags — the seeder resumes from the last
completed phase automatically.

## License

Source code: MIT.
Data re-distributed under their original licenses (see Data sources above).
