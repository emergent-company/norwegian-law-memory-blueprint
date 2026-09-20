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
└── cmd/
    └── seeder/
        └── main.go                  # standalone Lovdata seeder + seed exporter
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

The bundle is roughly **100k objects / 120k relationships** (laws, regulations,
provisions, ministries, legal areas and their relationships). `LegalParagraph`
and the relationship files dominate the size.

Alternatively, `memory blueprints dump <output-dir>` exports any already-populated
project back into the same `seed/objects/*.jsonl` + `seed/relationships/*.jsonl`
format.

## Data sources

| Source | License |
|---|---|
| [Lovdata public datasets](https://lovdata.no/info/utviklerinfo) (`gjeldende-lover.tar.bz2`, `gjeldende-sentrale-forskrifter.tar.bz2`) | NLOD 2.0 |
| [EUR-Lex](https://eur-lex.europa.eu) directive metadata | Public |
| [Publications Office EuroVoc SPARQL](https://publications.europa.eu/webapi/rdf/sparql) | Public |

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
`--cache-dir <dir>`, `--ingest-only`.

### Ingest directly into a Memory project

```bash
./seeder \
  --server  http://localhost:3012 \
  --token   <project-api-token> \
  --project <project-id>
```

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
| `--project` | `MEMORY_PROJECT_ID` | — | Project ID (required unless `--dump-seed`) |
| `--dump-seed` | `SEED_DUMP_DIR` | — | Export portable seed JSONL to `<dir>/seed/` instead of uploading |
| `--state-dir` | `MEMORY_STATE_DIR` | `~/.norwegian-law-seed-state` | Checkpoint directory |
| `--cache-dir` | `LOVDATA_CACHE_DIR` | `/tmp/lovdata_data` | Download cache |
| `--limit` | `SEED_LIMIT` | `0` (all) | Max documents per dataset |
| `--skip-eu` | — | false | Skip EUR-Lex / EuroVoc enrichment |
| `--eu-limit` | — | `0` (all) | Max EU directives to fetch |
| `--workers` | — | `20` | Parallel upload workers |
| `--batch` | — | `100` | Bulk API batch size |
| `--ingest-only` | — | false | Skip download; use cached archive |
| `--dataset` | — | `both` | `laws`, `regulations`, or `both` |

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
