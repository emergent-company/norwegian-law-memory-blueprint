# Coverage & limitations

This document records what the committed seed corpus does **not** contain, so
consumers do not over-trust empty results. A citation-verification or integrity
run that reports "not found" / "0 violations" is only meaningful within the
bounds below.

## What is not in the corpus

- **No court decisions / case law.** Only primary legislation (acts, central
  regulations, EU directive metadata) is ingested. There are no `Høyesterett` /
  appellate / district-court judgments and no `Rt.` / `HR-` citations.
- **No preparatory works (`forarbeider`).** NOU / Ot.prp. / Prop. reports and
  committee documents are absent. Ingesting them is tracked in **issue #12**.
- **No municipal or regional regulations.** Only *central* government
  regulations (`forskrift/…` from Lovdata's `gjeldende-sentrale-forskrifter`)
  are present. County/municipal bylaws and local regulations are out of scope.
- **EU directive text is partial.** EU/EEA directive nodes carry metadata
  (title, form, author, dates, subject matter, OJ reference, EuroVoc
  descriptors) and, for **63 of 85** directives, a `content` body sourced from
  CELLAR. The other 22 have no retrievable full text, and even the stored body is
  not guaranteed complete — do not treat an EU directive node as authoritative
  quoted EU law.
- **Many EU directive nodes are stubs.** Cited or amending acts that are not
  themselves among the referenced directives are stored as stub nodes
  (`celex_id` + `name` only — **534 of 619** `EUDirective` objects) so that
  `EU_CITES` / `EU_MODIFIED_BY` edges resolve instead of dangling. A stub carries
  no metadata.

## Temporal scope: current consolidated text only

- The corpus holds **only the current, consolidated text** from Lovdata's public
  datasets (`gjeldende-lover`, `gjeldende-sentrale-forskrifter`).
- There are **no historical or pre-amendment snapshots** and no "law as in force
  at date D" resolution. `last_change_in_force` records the latest amendment
  date, but earlier versions of a provision are not retained.
- Case law and historical versioning are **behind Lovdata Pro** and are not part
  of the public NLOD datasets.

## Known parse limits

- **~42,800 `LegalParagraph` rows (31%) still have an empty `paragraph_num`.**
  These are historic/edge acts whose headers carry no `legalArticleValue` (or a
  non-`§` numbering such as `ledd`/`kapittel`). Their `section_id` is populated,
  but section-number (`§ N` / `§ N-M`) lookups against them cannot resolve.
- **Footnotes and the table of contents are intentionally excluded** from the
  rendered `content` Markdown. The body text of provisions is preserved, but
  footnote text and the source TOC are dropped.
- **Heading structure is Markdown-rendered, not a lossless XML tree.** The
  Lovdata HTML class hierarchy (`legalArticle`, `legalP`, `ledd`, …) is flattened
  into `##`/`###` headings. Original HTML attributes and nested structure beyond
  what the renderer emits are not recoverable.

## EU/EEA metadata limits

- Metadata is sourced from **CELLAR / EUR-Lex SPARQL** (EU Publications Office).
  **Norwegian titles are not available**: CELLAR exposes EU official-language
  expressions only, so `full_title` is English. Norwegian/EØS wording must come
  from Lovdata/EFTA.
- **EU node identity is `celex_id`** (uppercase, e.g. `32014L0026`). The former
  `directive_id` property was **removed** in this change: the human-readable form
  (with its language suffix, e.g. `2014/26/EU`) is not recoverable from CELLAR,
  and deriving a partial form would invent data. Use `name` / `full_title` for
  display, and `celex_id` for joins.
- **EuroVoc labels are English only** (`label_en`); EuroVoc has no Norwegian
  labels. Concept keys use the numeric notation (`eurovoc_<id>`).
- `directory_code` holds CELLAR's human-readable label, not the numeric
  directory notation, and CELLAR's classification is occasionally
  counter-intuitive — treat it as authoritative-but-verified.
- `oj_reference` carries the OJ issue/number and start page; the **page range**
  (e.g. `26–31`) is not recoverable from the OJ document id.
- `responsible_dg` (80/85) and `procedure_num` (81/85) are absent for a few acts
  because CELLAR does not carry them.
- `HAS_LANGUAGE_VARIANT` edges are **0**: language variants are deduplicated
  ("first occurrence wins"), so no variant pairs exist to link.

## Source & licensing

- Data source: **Lovdata public datasets**, licensed under **NLOD 2.0** (Norsk
  lisens for offentlige data). We publish a **derivative** — parsed and rendered
  into this seed — not the raw Lovdata XML. See `NOTICE`.
- EUR-Lex directive metadata and EuroVoc thesaurus data are from the EU
  Publications Office (public).

## Related issues

- Preparatory works (`forarbeider`) ingestion — **issue #12**
- Citation verification tooling — **issue #11**, **issue #13**
- Corpus integrity checker (`lovcheck`) — **issue #13**
