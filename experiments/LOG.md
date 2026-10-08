# Experiment log

Newest last. Metric columns: `F1 / recall / precision / res_rate`. Probe = the
6-item fixed probe (see `README.md`); other datasets are named. Verdict follows
the keep/revert rule: **keep only if the primary metric (F1) improves without
losing recall**.

---

## E0 — baseline (no changes)
- **Date**: 2026-10-08
- **Dataset**: committed set (72 items)
- **Metrics**: `0.229 / 0.638 / 0.148 / 0.392`
- **Notes**: pre-fix baseline; extraction mis-attributed law names to amendment
  acts and the prompt had no citation discipline.
- **Verdict**: n/a (reference).

## E1 — abbreviation map stops stealing tokens to amendment acts
- **Change**: `internal/evaldata/abbrev.go` — skip amendment/opphøving acts;
  prefer exact short-title/parenthetical over hyphen fragments; deterministic
  tie-break. `refs.go` — recognise explicit `lov/…` keys.
- **Dataset**: weak subset (skatterett/personvern/gjeldsrett)
- **Before** (per area, from E0): skatterett `0.009 / 0.125`; personvern
  `0.111 / 0.167`; gjeldsrett `0.128 / 0.550`
- **After**: skatterett `0.347 / 1.000`; personvern `0.256 / 0.500`; gjeldsrett
  `0.148 / 0.500`
- **Verdict**: **KEEP** (root cause of near-zero recall; skatterett recall 0.125→1.0).
- **Commit**: `842b68f`

## E2 — cite selectively
- **Change**: prompt — name only the 1–5 provisions actually relied on; extras to
  a separate "Relaterte bestemmelser" list.
- **Dataset**: weak subset
- **Before**: `0.242 / — / 0.158 / —`; **After**: `0.495 / — / 0.374 / —`
- **Verdict**: **KEEP** (F1 ↑, precision ↑).
- **Commit**: `8e39edf`

## E3 — act-first retrieval
- **Change**: prompt — identify the governing act before looking for a section;
  scope the lookup to that act.
- **Dataset**: hard subset (10 agency/legal-aid items)
- **Result**: recall `1.000`, F1 `0.463`, precision `0.315`. Previously
  `fkjl §29`, `avtl §36`, `tvl §7-1` were not in top-100 for any topical query.
- **Verdict**: **KEEP**.
- **Commit**: `194d323`

## E4 — robust gold-ref extraction (generic tokens + bounded attribution)
- **Change**: `abbrev.go` — denylist generic tokens (`loven`, `avtalen`, …);
  `refs.go` — same-line attribution only, bounded "heading line" carry.
- **Dataset**: UiO generated (599 items), offline
- **Result**: gold_refs `1177 → 927` resolved, unresolved `929 → 116`; false
  `CFE-avtalen`/`EØS-loven` refs removed from `uio-jur1285-h21`; integrity PASS;
  rebuild byte-identical (sha256).
- **Verdict**: **KEEP** (false refs removed; +8 items with zero refs only).
- **Commit**: `17dd089`

## E5 — single-tool ablation (finding, no agent change)
- **Change**: grant exactly one retrieval tool at a time; same prompt.
- **Dataset**: probe (6)
- **Metrics**:
  | tools | F1 | recall | prec | res |
  |---|---|---|---|---|
  | search-hybrid only | 0.486 | 1.000 | 0.336 | 0.932 |
  | search-semantic only | 0.519 | 0.917 | 0.400 | 0.908 |
  | search-knowledge only | 0.477 | 0.833 | 0.377 | 0.833 |
  | full toolset (19) | 0.525 | 0.833 | 0.420 | 0.994 |
- **Findings**: the agent already uses params well (act-scoped queries,
  `types:["LegalParagraph"]`, `field_strategy:"full"`, `limit` 3–25).
  `search-hybrid` gives the best recall; the **full toolset gives the worst
  recall** (`entity-query`/`entity-search` anchoring). Over-citation is the main
  precision drain in every variant.
- **Verdict**: finding only; informs the lean-toolset hypothesis (E7).

## E6 — retrieval → verify → answer recipe
- **Change**: prompt — 5-step recipe (name act, find act, one act-scoped
  provision query, verify content, cite only verified ≤5).
- **Dataset**: probe (6)
- **Before → After**:
  - hybrid-only: `0.486 → 0.583` F1, `1.000 → 1.000` recall, `0.336 → 0.471` prec
  - full toolset: `0.525 → 0.531` F1, `0.833 → 0.917` recall, `0.420 → 0.403` prec
- **Verdict**: **KEEP** (hybrid F1 +0.097; full-set recall +0.084; control F1
  within noise).
- **Commit**: `c0d8ede`

---

## Proposed / not yet run
- **E7 — lean toolset**: `search-hybrid` + `entity-search` only (no
  `entity-query`). Hypothesis from E5: recovers recall lost by full toolset at
  lower cost.
- **E8 — hallucination guard**: prompt — cite a § only if its text was read; if
  that text names a different act, attribute there; never reuse a section number
  across acts. Targets non-resolving refs seen in E5/E6 (e.g. `sktl §5-30` leaking
  to `energiloven`/`mva`).
- **E9 — gold_refs quality gate for the UiO set**: only emit refs whose § is
  adjacent to the act mention (already tightened in E4); measure residual
  precision by spot-checking N veiledninger.
