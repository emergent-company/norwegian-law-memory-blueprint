# tests/eval — norwegian-law-assistant evaluation harness

Deterministic, offline-first evaluation for the `norwegian-law-assistant` agent.
Two tests:

1. **`TestDatasetIntegrity`** — always runs, no credentials, no network. Validates
   the committed eval set (`evals/golden/core.jsonl` + `evals/curated/*.jsonl`).
2. **`TestAgentEval`** — live agent eval. Gated behind credentials; `t.Skip`s when
   they are absent.

## Run offline (no credentials)

```sh
go test ./tests/eval/ -run TestDatasetIntegrity -v -count=1

# Validate a different set offline, e.g. a freshly built one:
EVAL_DATASET=evals/dataset/uio.jsonl \
  go test ./tests/eval/ -run TestDatasetIntegrity -v -count=1
```

## Run live (against a deployed agent)

```sh
TEST_SERVER_URL=https://… \
TEST_API_TOKEN=… \
go test ./tests/eval/ -run TestAgentEval -v -count=1 -timeout 60m
```

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `TEST_SERVER_URL` | — | A2A server base URL (fallback `MEMORY_SERVER_URL`). |
| `TEST_API_TOKEN` | — | Bearer token (fallback `MEMORY_PROJECT_API_KEY`). |
| `EVAL_DATASET` | `evals/golden/core.jsonl` | Dataset spec: a comma-separated list of paths/globs, relative to the repo root (or absolute). E.g. `evals/golden/core.jsonl,evals/curated/*.jsonl`. |
| `EVAL_LIMIT` | all | Cap the number of items run (smoke-testing). `0`/unset = all. |
| `EVAL_MIN_CITATION_F1` | `0` | Fail the test if overall citation F1 is below this. |
| `EVAL_MIN_POINT_RECALL` | `0` | Fail the test if overall point recall (when judged) is below this. |
| `JUDGE_BASE_URL` | — | OpenAI-compatible base URL; enables the LLM judge. |
| `JUDGE_MODEL` | — | Model name for the judge. |
| `JUDGE_API_KEY` | — | Optional API key (`Authorization: Bearer`). |
| `JUDGE_EXTRA_HEADERS` | — | Optional JSON object of extra headers for the judge request. |
| `LAW_BLUEPRINT_DIR` | auto | Override repo root (used to locate `seed/` and `evals/`). |

## Dataset schema

JSONL files, one `Item` per line (defined in `internal/evaldata/item.go`):

```jsonc
{
  "id": "core-aml-oppsigelsesfrist-001",
  "question": "…",            // plain-text Norwegian question
  "gold_points": ["…", "…"],  // expected answer points
  "gold_refs": ["lov/2005-06-17-62#§15-3"], // canonical act#§section refs
  "legal_area": "arbeidsrett",
  "difficulty": "easy",
  "answerable": true,         // false => refusal-safety item, gold_refs must be []
  // … plus course/semester/language/source/source_url/license/needs_curation
}
```

## Scoring definitions

**Citation score (deterministic, primary).** References are extracted from the
answer with `evaldata.ExtractRefs` (abbreviation-aware), resolved against the seed
with `evaldata.ResolveCandidates`/`lovcite`, and matched to `gold_refs` after
normalisation:

- **exact** = same act key + same section → 1.0
- **partial** = same act, different/no section → 0.5
- otherwise → 0

Per-item **precision** = mean best-match of each cited ref against gold;
**recall** = mean best-match of each gold ref against cited; **F1** = harmonic mean.
**resolution_rate** = resolved / total cited (1.0 when nothing cited).
**hallucinated_refs** = cited refs that do not resolve against the seed.

**Answer quality (LLM judge, optional).** Enabled only when `JUDGE_BASE_URL` and
`JUDGE_MODEL` are set. POSTs an OpenAI-compatible `/chat/completions` request
asking for a strict JSON verdict `{"covered","total","faithful","reason"}`.
`point_recall = covered/total ∈ [0,1]`. When the judge is unset (or errors),
`point_recall` is `null` and the test still passes.

**Refusal (safety, `answerable:false`).** Pass when the answer states it cannot
find / does not contain the answer **and** cites no invented (hallucinated)
provision. Fail when it asserts a rule.

**Aggregates** are computed overall, per `legal_area`, and per `difficulty`,
covering citation F1/recall/precision, resolution rate, point recall (when
judged), and refusal-correct count.

## Report

A machine-readable report is written to `evals/results/<UTC-timestamp>.json`
(gitignored). The filename timestamp is the only time-dependent part; all other
output (item order, group ordering, JSON map keys) is deterministic.

## Adding items

Append a JSON line to an existing `evals/*.jsonl` file (or add a new file and
include it via `EVAL_DATASET`). Rules:

- `id` must be unique across all files (`TestDatasetIntegrity` enforces this).
- `answerable:true` items must have a non-empty `question` and non-empty
  `gold_points`, and every `gold_refs` entry must resolve against `seed/`.
- `answerable:false` items must have an empty `gold_refs`.

## Licensing

The underlying data originates from University of Oslo exam materials and public
Norwegian agency guidance. It is public but **copyrighted**; this eval set is for
**internal evaluation only** and must not be redistributed.
