# EU reference model

How the Norwegian↔EU cross-references (and the adjacent amendment/version
metadata) are represented in this blueprint's knowledge graph. Grounded in the
shipped `schemas/norwegian-law.yaml` and the regenerated `seed/`.

## EUDirective objects

`EUDirective` is the single node type for EU/EEA acts referenced by Norwegian
law — directives, regulations and decisions alike (the type name is historical;
it is keyed by `celex_id`, not by legal form).

Base metadata (from CELLAR / EUR-Lex): `name`, `full_title`, `form`,
`date_of_document`, `date_of_effect`, `author`, `responsible_dg`,
`subject_matter`, `directory_code`, `legal_basis`, `procedure_num`,
`oj_reference`, `content` (Markdown, when retrievable).

Derived fields (deterministic, computed from `celex_id`):

| Field | Derivation |
|---|---|
| `community` | CELEX year: `< 1993` → `EEG`, `1993–2009` → `EG`, `>= 2009` → `EU` |
| `url_eur_lex` | ELI URL (see below) |
| `in_force` | always `true` — no repeal/expiry signal is extracted |
| `title_no` | always absent — no Norwegian title source |

## CELEX identity and the ELI URL

CELEX ids are uppercase and of the form `3YYYY<T>NNNN`, where:

- `3` = sector 3 (legislation),
- `YYYY` = four-digit year,
- `<T>` = type letter: `L` directive, `R` regulation, `D` decision,
- `NNNN` = zero-padded number.

Examples: `32014L0026` (Directive 2014/26/EU), `32009R0469`, `32008D0768`.

The canonical ELI URL is derived as:

```
https://eur-lex.europa.eu/eli/{dir|reg|dec}/YYYY/N
```

where the segment maps `L`→`dir`, `R`→`reg`, `D`→`dec` and `N` is the
zero-trimmed number (`32014L0026` → `https://eur-lex.europa.eu/eli/dir/2014/26`).

## Cross-reference edges and `reference_type`

Two edge types link Norwegian documents to EU acts. Memory relationship types
carry **no declarative properties** (the loader's `RelationshipTypeDef` has no
`properties` field), so the attributes below are free-form edge data in the seed
JSONL — documented here because they are not expressible in the YAML schema.

### `IMPLEMENTS_EEA` (Law/Regulation → EUDirective)

Sourced from the document's EEA header (`source:"eea_references"`). Properties:

- `source` — `"eea_references"`.
- `reference_type` — the classification of the reference (see values below).
- `is_primary` — `true` when the edge is a transposition declaration.
- `implementation_status` — always `"unknown"` (transposition is not verified).
- `eu_articles` — article refs found near the reference, comma-joined.
- `reference_context` — the surrounding clause, capped at 240 chars.
- `trigger_keyword` — the lowercased matched keyword (empty when none).

A header reference is itself a transposition declaration, so the default is
`reference_type:"implements"` + `is_primary:true` when no keyword is found. When
an explicit keyword is present, `reference_type` follows the mapping below and
`is_primary` is `true` only for `implements`/`supplements`.

### `CITES_EU_LAW` (Law/Regulation → EUDirective)

Sourced from inline body citations. Properties: `reference_type` (default
`cites_article`), `eu_articles`, `reference_context`, `trigger_keyword`.

### `reference_type` values

| Value | Meaning |
|---|---|
| `implements` | The Norwegian act transposes the EU act (`gjennomfør…`). |
| `supplements` | The act supplements / fills out the EU act (`suppler`, `utfyll`). |
| `applies` | The EU act applies directly (`gjelder direkte`, `anvendes direkte`). |
| `references` | A generic cross-reference (`viser til`, `henviser til`). |
| `complies_with` | The act states compliance (`i samsvar med`, `oppfyller`). |
| `derogates_from` | The act derogates (`fravik`). |
| `amended_by` | The EU act was amended (`endret ved`, `endring av`). |
| `repealed_by` | The EU act was repealed (`opphevet ved`, `oppheving av`). |
| `cites_article` | Default for a body citation to an EU article. |

## Amendment & version metadata

- **`Amendment`** — a statutory change record (`name`, `amended_by_ref_id`,
  `amendment_date`, `amendment_type`, `target_provision_ref`,
  `target_law_ref_id`, `change_summary`, `source`). Law-level records are
  derived from Lovdata `last_change_in_force` / `AMENDS` metadata
  (`source:"metadata"`); per-provision records come from
  "… ved lov D. MMM YYYY nr. N" body notes (`source:"body_note"`). `HAS_AMENDMENT`
  links a law to its `Amendment` nodes (targets are filtered to the corpus).
- **`ProvisionVersion`** — a temporal anchor for a provision (`name`,
  `paragraph_ref`, `paragraph_section_id`, `valid_from`, `valid_to`, `status`,
  `parent_law_ref_id`). Emitted only for provisions with a body note;
  `valid_to` is never set and `status` is always `current` (no historical
  snapshots). `HAS_VERSION` links a law to its version anchors.

## Query intents

- **EU basis of a law** — follow `IMPLEMENTS_EEA` from a `Law` to the
  `EUDirective` it transposes, then read `full_title`, `url_eur_lex`, and the
  edge's `reference_type`/`eu_articles`.
- **Norwegian implementations of an EU act** — reverse `IMPLEMENTS_EEA`
  (`is_primary:true` for the transposing acts), or `CITES_EU_LAW` for acts that
  merely cite it.
- **Which provisions cite an EU act** — resolve `CITES_EU_LAW` targets and read
  the edge's `reference_context`/`eu_articles` to locate the citing provision.
- **What amended a law** — `AMENDED_BY` (last amendment) / `AMENDS` (acts it
  amends) for the relationship, and `HAS_AMENDMENT` for the detailed
  `Amendment` records (date, type, source).
