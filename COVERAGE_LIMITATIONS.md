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
- **No EU act full text.** We ingest EU/EEA *directive metadata* and link it via
  `IMPLEMENTS_EEA` / `CITES_EU_LAW`, but we do **not** store the directive's
  legislative text as a navigable object. Do not treat an EU directive node as
  a source of quoted EU text.

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
